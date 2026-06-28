/**
 * Layer 1 SQL contract tests for the Note domain.
 *
 * Notes are exclusive subtypes — every Note row is paired with exactly
 * one of Project_Note / Milestone_Note / Task_Note. The discriminator
 * CHECK calls fn_NoteIsOfType, so wedging a Project_Note row pointing
 * at a Note whose note_type is 'milestone' must fail with a constraint
 * error. This file exercises the three Create variants, Update, the
 * relevance state machine, Delete, and the discriminator.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';

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
        name: `note-agent-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Agent_Create returned no rows');
    }

    return row.agent_id;

}

async function createProject(): Promise<number> {

    const rows = await ctx.proc('sp_Project_Create', {
        name: `note-project-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Project_Create returned no rows');
    }

    return row.project_id;

}

async function createMilestone(): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title: `note-milestone-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Milestone_Create returned no rows');
    }

    return row.milestone_id;

}

async function createTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId,
        title: `note-task-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Task_Create returned no rows');
    }

    return row;

}

describe('sql.note: create variants', () => {

    it('sp_Note_Create_Project inserts Note + Project_Note with note_type=project', async () => {

        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Project', {
            content: 'project-scoped note',
            project_id: projectId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('sp_Note_Create_Project returned no rows');
        }

        expect(noteId).toBeGreaterThan(0);

        const note = await ctx.kysely
            .selectFrom('Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.note_type).toBe('project');
        expect(note?.relevance_status).toBe('active');

        const subtype = await ctx.kysely
            .selectFrom('Project_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(subtype?.project_id).toBe(projectId);

    });

    it('sp_Note_Create_Milestone inserts Note + Milestone_Note with note_type=milestone', async () => {

        const milestoneId = await createMilestone();

        const rows = await ctx.proc('sp_Note_Create_Milestone', {
            content: 'milestone-scoped note',
            milestone_id: milestoneId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('sp_Note_Create_Milestone returned no rows');
        }

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['note_type'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.note_type).toBe('milestone');

        const subtype = await ctx.kysely
            .selectFrom('Milestone_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(subtype?.milestone_id).toBe(milestoneId);

    });

    it('sp_Note_Create_Task inserts Note + Task_Note with note_type=task', async () => {

        const milestoneId = await createMilestone();
        const task = await createTask(milestoneId);

        const rows = await ctx.proc('sp_Note_Create_Task', {
            content: 'task-scoped note',
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('sp_Note_Create_Task returned no rows');
        }

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['note_type'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.note_type).toBe('task');

        const subtype = await ctx.kysely
            .selectFrom('Task_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(subtype?.milestone_id).toBe(task.milestone_id);
        expect(subtype?.task_no).toBe(task.task_no);

    });

});

describe('sql.note: update + state machine + delete', () => {

    it('sp_Note_Update changes content and is observable', async () => {

        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Project', {
            content: 'before update',
            project_id: projectId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Note_Update', {
            note_id: noteId,
            content: 'after update',
            reason: 'because',
        });

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['content', 'reason'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.content).toBe('after update');
        expect(note?.reason).toBe('because');

    });

    it('sp_Note_SetRelevance happy path moves active -> needs-review and writes audit row', async () => {

        const agentId = await createAgent();
        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Project', {
            content: 'state machine subject',
            project_id: projectId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Note_SetRelevance', {
            note_id: noteId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['relevance_status'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.relevance_status).toBe('needs-review');

        const audit = await ctx.kysely
            .selectFrom('Note_StateTransition as nst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'nst.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.state_transition_type'])
            .where('nst.note_id', '=', noteId)
            .executeTakeFirst();

        expect(audit?.from_status).toBe('active');
        expect(audit?.to_status).toBe('needs-review');
        expect(audit?.state_transition_type).toBe('note-relevance');

    });

    it('sp_Note_Delete soft-deletes via the relevance state machine', async () => {

        const agentId = await createAgent();
        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Project', {
            content: 'to be deleted',
            project_id: projectId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Note_Delete', {
            note_id: noteId,
            agent_id: agentId,
        });

        const note = await ctx.kysely
            .selectFrom('Note')
            .select(['relevance_status'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(note?.relevance_status).toBe('deleted');

    });

});

describe('sql.note: restore', () => {

    it('sp_Note_Restore flips relevance_status from deleted back to active', async () => {

        const agentId = await createAgent();
        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Project', {
            content: 'restoration subject',
            project_id: projectId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Note_Delete', {
            note_id: noteId,
            agent_id: agentId,
        });

        const deleted = await ctx.kysely
            .selectFrom('Note')
            .select(['relevance_status'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(deleted?.relevance_status).toBe('deleted');

        await ctx.proc('sp_Note_Restore', {
            note_id: noteId,
            agent_id: agentId,
        });

        const restored = await ctx.kysely
            .selectFrom('Note')
            .select(['relevance_status'])
            .where('note_id', '=', noteId)
            .executeTakeFirst();

        expect(restored?.relevance_status).toBe('active');

        const transitions = await ctx.kysely
            .selectFrom('Note_StateTransition as nst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'nst.transition_id')
            .select(['st.from_status', 'st.to_status'])
            .where('nst.note_id', '=', noteId)
            .orderBy('st.transition_id')
            .execute();

        expect(transitions.length).toBe(2);
        expect(transitions[0]?.to_status).toBe('deleted');
        expect(transitions[1]?.from_status).toBe('deleted');
        expect(transitions[1]?.to_status).toBe('active');

    });

});

describe('sql.note: discriminator CHECK constraint', () => {

    it('rejects a Project_Note insert when the parent Note has note_type != project', async () => {

        // First create a milestone-typed Note via the proper proc.
        const milestoneId = await createMilestone();
        const projectId = await createProject();

        const rows = await ctx.proc('sp_Note_Create_Milestone', {
            content: 'wrong-typed parent',
            milestone_id: milestoneId,
        });

        const noteId = rows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        // Now try to insert a Project_Note row pointing at this milestone-typed
        // Note. The CK_Project_Note_NoteType CHECK calls fn_NoteIsOfType and
        // returns 0 because note_type='milestone', so the insert is rejected.
        await expect(
            ctx.kysely
                .insertInto('Project_Note')
                .values({ note_id: noteId, project_id: projectId })
                .execute(),
        ).rejects.toThrow(/CHECK constraint|CK_Project_Note_NoteType/i);

    });

});
