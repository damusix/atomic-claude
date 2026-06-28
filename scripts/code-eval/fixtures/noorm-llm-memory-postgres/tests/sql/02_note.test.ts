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

async function makeTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: 'T', p_content: '',
        p_reason: 'fixture', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

describe('sp_Note_Create_Project', () => {

    it('creates a Note row plus a Project_Note subtype row', async () => {

        const result = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'project kickoff', p_reason: 'docs',
            p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [created] = result;
        if (!created) throw new Error('note create failed');

        const note = await ctx.kysely.selectFrom('Note')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(note.note_type).toBe('project');
        expect(note.relevance_status).toBe('active');

        const subtype = await ctx.kysely.selectFrom('Project_Note')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(subtype.project_id).toBe(0);

    });

});

describe('sp_Note_Create_Milestone', () => {

    it('creates a Note plus a Milestone_Note subtype row', async () => {

        const milestoneId = await makeMilestone();

        const result = await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'm-note', p_reason: 'docs',
            p_provenance_id: 0, p_agent_id: 0, p_milestone_id: milestoneId,
        });
        const [created] = result;
        if (!created) throw new Error('note create failed');

        const note = await ctx.kysely.selectFrom('Note')
            .select('note_type')
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(note.note_type).toBe('milestone');

        const sub = await ctx.kysely.selectFrom('Milestone_Note')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(sub.milestone_id).toBe(milestoneId);

    });

});

describe('sp_Note_Create_Task', () => {

    it('creates a Note plus a Task_Note subtype row', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        const result = await ctx.proc('sp_Note_Create_Task', {
            p_content: 't-note', p_reason: 'docs',
            p_provenance_id: 0, p_agent_id: 0,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });
        const [created] = result;
        if (!created) throw new Error('note create failed');

        const sub = await ctx.kysely.selectFrom('Task_Note')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(sub.milestone_id).toBe(task.milestone_id);
        expect(sub.task_no).toBe(task.task_no);

    });

});

describe('sp_Note_Update', () => {

    it('rewrites content + reason but leaves note_type/relevance unchanged', async () => {

        const r = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'before', p_reason: 'r1',
            p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [created] = r;
        if (!created) throw new Error('note create failed');

        await ctx.proc('sp_Note_Update', {
            p_note_id: created.note_id, p_content: 'after', p_reason: 'r2',
        });

        const note = await ctx.kysely.selectFrom('Note')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(note.content).toBe('after');
        expect(note.reason).toBe('r2');
        expect(note.note_type).toBe('project');
        expect(note.relevance_status).toBe('active');

    });

});

describe('sp_Note_SetRelevance', () => {

    it('moves active -> needs-review and writes a Note_StateTransition row', async () => {

        const r = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'x', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [created] = r;
        if (!created) throw new Error('note create failed');

        await ctx.proc('sp_Note_SetRelevance', {
            p_note_id: created.note_id,
            p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const note = await ctx.kysely.selectFrom('Note')
            .select('relevance_status')
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(note.relevance_status).toBe('needs-review');

        const subtype = await ctx.kysely.selectFrom('Note_StateTransition')
            .selectAll()
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        const transition = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype.transition_id)
            .executeTakeFirstOrThrow();

        expect(transition.state_transition_type).toBe('note-relevance');

    });

    it('rejects an unallowed transition (active -> superseded -> active is not directly allowed but we use needs-review -> needs-review)', async () => {

        const r = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'x', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [created] = r;
        if (!created) throw new Error('note create failed');

        await ctx.proc('sp_Note_SetRelevance', {
            p_note_id: created.note_id,
            p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'first',
        });

        // Proc raises 'transition needs-review -> needs-review not allowed for note-relevance'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Note_SetRelevance', {
                p_note_id: created.note_id,
                p_new_relevance_status: 'needs-review',
                p_agent_id: 0, p_reason: 'should reject',
            }),
        ).rejects.toThrow(/transition needs-review -> needs-review not allowed for note-relevance/);

    });

});

describe('sp_Note_Delete + sp_Note_Restore', () => {

    it('round-trips active -> deleted -> active', async () => {

        const r = await ctx.proc('sp_Note_Create_Project', {
            p_content: 'x', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
        });
        const [created] = r;
        if (!created) throw new Error('note create failed');

        await ctx.proc('sp_Note_Delete', {
            p_note_id: created.note_id, p_agent_id: 0, p_reason: 'cleanup',
        });

        const deleted = await ctx.kysely.selectFrom('Note')
            .select('relevance_status')
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(deleted.relevance_status).toBe('deleted');

        await ctx.proc('sp_Note_Restore', {
            p_note_id: created.note_id, p_agent_id: 0, p_reason: 'kept',
        });

        const restored = await ctx.kysely.selectFrom('Note')
            .select('relevance_status')
            .where('note_id', '=', created.note_id)
            .executeTakeFirstOrThrow();

        expect(restored.relevance_status).toBe('active');

    });

});

describe('Note exclusivity trigger', () => {

    it('rejects inserting a Project_Note row for a milestone-typed note', async () => {

        const milestoneId = await makeMilestone();

        const r = await ctx.proc('sp_Note_Create_Milestone', {
            p_content: 'mn', p_reason: '',
            p_provenance_id: 0, p_agent_id: 0, p_milestone_id: milestoneId,
        });
        const [created] = r;
        if (!created) throw new Error('milestone-note create failed');

        // Trigger raises 'trg_check_note_exclusivity: note_id N has note_type=milestone
        // but is being written into Project_Note (expected note_type=project)'.
        await expect(
            ctx.kysely.insertInto('Project_Note')
                .values({ note_id: created.note_id, project_id: 0 })
                .execute(),
        ).rejects.toThrow(/trg_check_note_exclusivity.*note_type=milestone.*Project_Note.*expected note_type=project/);

    });

});
