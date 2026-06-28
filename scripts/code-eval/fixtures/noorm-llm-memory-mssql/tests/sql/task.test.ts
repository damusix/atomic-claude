/**
 * Layer 1 SQL contract tests for the Task domain.
 *
 * Task is a hierarchic child of Milestone with composite PK
 * (milestone_id, task_no). task_no is auto-allocated by fn_NextTaskNo
 * inside sp_Task_Create, so we verify it monotonically increments per
 * milestone. Dependencies are validated against the verb whitelist and
 * a cycle check via fn_TaskDependencyWouldCycle.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { tvp } from '@noormdev/sdk';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});



beforeEach(async () => {

    await resetApplicationData(ctx);

});

async function createAgent(): Promise<number> {

    const rows = await ctx.proc('sp_Agent_Create', {
        name: `task-agent-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('agent creation failed');
    }

    return row.agent_id;

}

async function createMilestone(): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title: `task-milestone-${Date.now()}-${Math.random()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('milestone creation failed');
    }

    return row.milestone_id;

}

async function createTask(milestoneId: number, title: string): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId,
        title,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('task creation failed');
    }

    return row;

}

describe('sql.task: CRUD', () => {

    it('sp_Task_Create allocates task_no = 1 then 2 under the same milestone', async () => {

        const milestoneId = await createMilestone();

        const a = await createTask(milestoneId, 'first task');
        const b = await createTask(milestoneId, 'second task');

        expect(a.milestone_id).toBe(milestoneId);
        expect(a.task_no).toBe(1);
        expect(b.milestone_id).toBe(milestoneId);
        expect(b.task_no).toBe(2);

        const rows = await ctx.kysely
            .selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .orderBy('task_no')
            .execute();

        expect(rows.length).toBe(2);
        expect(rows[0]?.title).toBe('first task');
        expect(rows[0]?.tracking_status).toBe('not-started');
        expect(rows[1]?.title).toBe('second task');

    });

    it('sp_Task_Update mutates title/content/reason observably', async () => {

        const milestoneId = await createMilestone();
        const t = await createTask(milestoneId, 'original');

        await ctx.proc('sp_Task_Update', {
            milestone_id: t.milestone_id,
            task_no: t.task_no,
            title: 'updated',
            content: 'updated content',
            reason: 'because',
        });

        const row = await ctx.kysely
            .selectFrom('Task')
            .select(['title', 'content', 'reason'])
            .where('milestone_id', '=', t.milestone_id)
            .where('task_no', '=', t.task_no)
            .executeTakeFirst();

        expect(row?.title).toBe('updated');
        expect(row?.content).toBe('updated content');
        expect(row?.reason).toBe('because');

    });

});

describe('sql.task: delete (soft via abandoned)', () => {

    it('sp_Task_Delete soft-deletes the task by transitioning tracking_status to abandoned and cascades notes', async () => {

        const agentId = await createAgent();
        const milestoneId = await createMilestone();
        const t = await createTask(milestoneId, 'to-be-deleted');

        const noteRows = await ctx.proc('sp_Note_Create_Task', {
            content: 'attached note',
            milestone_id: t.milestone_id,
            task_no: t.task_no,
        });

        const noteId = noteRows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Task_Delete', {
            milestone_id: t.milestone_id,
            task_no: t.task_no,
            agent_id: agentId,
            reason: 'cleanup',
        });

        const task = await ctx.kysely
            .selectFrom('Task')
            .select(['tracking_status'])
            .where('milestone_id', '=', t.milestone_id)
            .where('task_no', '=', t.task_no)
            .executeTakeFirst();

        expect(task?.tracking_status).toBe('abandoned');

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['relevance_status'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.relevance_status).toBe('deleted');

    });

});

describe('sql.task: tracking state machine', () => {

    it('sp_Task_SetTracking moves not-started -> in-progress and writes audit row', async () => {

        const agentId = await createAgent();
        const milestoneId = await createMilestone();
        const t = await createTask(milestoneId, 'state subject');

        await ctx.proc('sp_Task_SetTracking', {
            milestone_id: t.milestone_id,
            task_no: t.task_no,
            new_tracking_status: 'in-progress',
            agent_id: agentId,
        });

        const row = await ctx.kysely
            .selectFrom('Task')
            .select(['tracking_status'])
            .where('milestone_id', '=', t.milestone_id)
            .where('task_no', '=', t.task_no)
            .executeTakeFirst();

        expect(row?.tracking_status).toBe('in-progress');

        const audit = await ctx.kysely
            .selectFrom('Task_StateTransition as tst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'tst.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.state_transition_type'])
            .where('tst.milestone_id', '=', t.milestone_id)
            .where('tst.task_no', '=', t.task_no)
            .executeTakeFirst();

        expect(audit?.from_status).toBe('not-started');
        expect(audit?.to_status).toBe('in-progress');
        expect(audit?.state_transition_type).toBe('task-tracking');

    });

});

describe('sql.task: dependencies', () => {

    it('sp_Task_Depend creates an edge and sp_Task_Undepend removes it', async () => {

        const milestoneId = await createMilestone();
        const a = await createTask(milestoneId, 'task A');
        const b = await createTask(milestoneId, 'task B');

        await ctx.proc('sp_Task_Depend', {
            milestone_id: a.milestone_id,
            task_no: a.task_no,
            dep_milestone_id: b.milestone_id,
            dep_task_no: b.task_no,
            dependency_verb: 'blocks',
            reason: 'A blocks B',
        });

        const before = await ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', a.milestone_id)
            .where('task_no', '=', a.task_no)
            .where('dep_milestone_id', '=', b.milestone_id)
            .where('dep_task_no', '=', b.task_no)
            .execute();

        expect(before.length).toBe(1);
        expect(before[0]?.dependency_verb).toBe('blocks');

        await ctx.proc('sp_Task_Undepend', {
            milestone_id: a.milestone_id,
            task_no: a.task_no,
            dep_milestone_id: b.milestone_id,
            dep_task_no: b.task_no,
        });

        const after = await ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', a.milestone_id)
            .where('task_no', '=', a.task_no)
            .where('dep_milestone_id', '=', b.milestone_id)
            .where('dep_task_no', '=', b.task_no)
            .execute();

        expect(after.length).toBe(0);

    });

    it('sp_Task_Depend rejects a would-be cycle', async () => {

        const milestoneId = await createMilestone();
        const a = await createTask(milestoneId, 'cycle A');
        const b = await createTask(milestoneId, 'cycle B');

        // A -> B is fine.
        await ctx.proc('sp_Task_Depend', {
            milestone_id: a.milestone_id,
            task_no: a.task_no,
            dep_milestone_id: b.milestone_id,
            dep_task_no: b.task_no,
            dependency_verb: 'blocks',
        });

        // B -> A would close a 2-cycle and must be rejected.
        await expect(
            ctx.proc('sp_Task_Depend', {
                milestone_id: b.milestone_id,
                task_no: b.task_no,
                dep_milestone_id: a.milestone_id,
                dep_task_no: a.task_no,
                dependency_verb: 'blocks',
            }),
        ).rejects.toThrow(/cycle/i);

    });

    it('sp_Task_Bulk_Depend inserts every TVP row', async () => {

        const milestoneId = await createMilestone();
        const root = await createTask(milestoneId, 'root');
        const childA = await createTask(milestoneId, 'child A');
        const childB = await createTask(milestoneId, 'child B');

        await ctx.proc('sp_Task_Bulk_Depend', {
            Deps: tvp('TaskDependencyInput', [
                {
                    milestone_id: root.milestone_id,
                    task_no: root.task_no,
                    dep_milestone_id: childA.milestone_id,
                    dep_task_no: childA.task_no,
                    dependency_verb: 'blocks',
                    reason: 'bulk-1',
                },
                {
                    milestone_id: root.milestone_id,
                    task_no: root.task_no,
                    dep_milestone_id: childB.milestone_id,
                    dep_task_no: childB.task_no,
                    dependency_verb: 'blocks',
                    reason: 'bulk-2',
                },
            ]),
        });

        const rows = await ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', root.milestone_id)
            .where('task_no', '=', root.task_no)
            .orderBy('dep_task_no')
            .execute();

        expect(rows.length).toBe(2);
        expect(rows[0]?.dep_task_no).toBe(childA.task_no);
        expect(rows[0]?.reason).toBe('bulk-1');
        expect(rows[1]?.dep_task_no).toBe(childB.task_no);
        expect(rows[1]?.reason).toBe('bulk-2');

    });

});
