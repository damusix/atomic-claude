import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeMilestone(): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: 'M', p_content: '', p_reason: 'fixture',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number, title = 'T'): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: title, p_content: '',
        p_reason: 'fixture', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

describe('sp_Task_Create', () => {

    it('returns the composite (milestone_id, task_no) and assigns sequential task_no', async () => {

        const milestoneId = await makeMilestone();

        const t1 = await makeTask(milestoneId, 'first');
        const t2 = await makeTask(milestoneId, 'second');

        expect(t1.milestone_id).toBe(milestoneId);
        expect(t1.task_no).toBe(1);
        expect(t2.task_no).toBe(2);

        const row = await ctx.kysely.selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', t1.task_no)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('first');
        expect(row.tracking_status).toBe('not-started');

    });

});

describe('sp_Task_Update', () => {

    it('rewrites title/content/reason but preserves tracking_status', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        await ctx.proc('sp_Task_Update', {
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
            p_title: 'updated', p_content: 'c', p_reason: 'r',
        });

        const row = await ctx.kysely.selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('updated');
        expect(row.tracking_status).toBe('not-started');

    });

});

describe('sp_Task_SetTracking', () => {

    it('walks not-started -> in-progress -> done and writes Task_StateTransition rows', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: 'go',
        });
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
            p_new_tracking_status: 'done', p_agent_id: 0, p_reason: 'fin',
        });

        const row = await ctx.kysely.selectFrom('Task')
            .select('tracking_status')
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirstOrThrow();

        expect(row.tracking_status).toBe('done');

        const subs = await ctx.kysely.selectFrom('Task_StateTransition')
            .selectAll()
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(subs.length).toBe(2);

    });

    it('rejects an unallowed transition (not-started -> done)', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        // Proc raises 'transition not-started -> done not allowed for task-tracking'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Task_SetTracking', {
                p_milestone_id: task.milestone_id, p_task_no: task.task_no,
                p_new_tracking_status: 'done', p_agent_id: 0, p_reason: 'reject',
            }),
        ).rejects.toThrow(/transition not-started -> done not allowed for task-tracking/);

    });

});

describe('sp_Task_Delete', () => {

    it('moves the task to abandoned via SetTracking', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        await ctx.proc('sp_Task_Delete', {
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
            p_agent_id: 0, p_reason: 'cleanup',
        });

        const row = await ctx.kysely.selectFrom('Task')
            .select('tracking_status')
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirstOrThrow();

        expect(row.tracking_status).toBe('abandoned');

    });

});

describe('sp_Task_Depend', () => {

    it('writes a Task_Dependency row for a valid edge', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
            p_dep_milestone_id: b.milestone_id, p_dep_task_no: b.task_no,
            p_dependency_verb:  'blocks',
            p_reason:           'a is blocked by b',
        });

        const edge = await ctx.kysely.selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', a.milestone_id)
            .where('task_no', '=', a.task_no)
            .where('dep_milestone_id', '=', b.milestone_id)
            .where('dep_task_no', '=', b.task_no)
            .executeTakeFirstOrThrow();

        expect(edge.dependency_verb).toBe('blocks');

    });

    it('rejects a self-dependency', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId);

        // Proc raises 'task cannot depend on itself' with SQLSTATE '22023'.
        await expect(
            ctx.proc('sp_Task_Depend', {
                p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
                p_dep_milestone_id: a.milestone_id, p_dep_task_no: a.task_no,
                p_dependency_verb:  'blocks',
                p_reason:           'self',
            }),
        ).rejects.toThrow(/task cannot depend on itself/);

    });

    it('rejects a cycle (A -> B -> A)', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        // A depends on B (so walking from B never reaches A → safe).
        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
            p_dep_milestone_id: b.milestone_id, p_dep_task_no: b.task_no,
            p_dependency_verb:  'blocks',
            p_reason:           'a needs b first',
        });

        // Now adding B depends on A would close A->B->A. Cycle check walks
        // from dep (A) and finds B (the originator) reachable.
        // Proc raises 'dependency would create a cycle' with SQLSTATE '23514'.
        await expect(
            ctx.proc('sp_Task_Depend', {
                p_milestone_id:     b.milestone_id, p_task_no:     b.task_no,
                p_dep_milestone_id: a.milestone_id, p_dep_task_no: a.task_no,
                p_dependency_verb:  'blocks',
                p_reason:           'cycle',
            }),
        ).rejects.toThrow(/dependency would create a cycle/);

    });

});

describe('sp_Task_Undepend', () => {

    it('removes the dependency row', async () => {

        const milestoneId = await makeMilestone();
        const a = await makeTask(milestoneId, 'a');
        const b = await makeTask(milestoneId, 'b');

        await ctx.proc('sp_Task_Depend', {
            p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
            p_dep_milestone_id: b.milestone_id, p_dep_task_no: b.task_no,
            p_dependency_verb:  'blocks',
            p_reason:           'first',
        });

        await ctx.proc('sp_Task_Undepend', {
            p_milestone_id:     a.milestone_id, p_task_no:     a.task_no,
            p_dep_milestone_id: b.milestone_id, p_dep_task_no: b.task_no,
        });

        const gone = await ctx.kysely.selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', a.milestone_id)
            .where('task_no', '=', a.task_no)
            .where('dep_milestone_id', '=', b.milestone_id)
            .where('dep_task_no', '=', b.task_no)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});
