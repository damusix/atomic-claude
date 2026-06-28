import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeMilestone(title = 'M1'): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: title, p_content: '', p_reason: 'fixture',
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

async function makeProject(name: string): Promise<number> {

    const result = await ctx.proc('sp_Project_Create', {
        p_name: name, p_filepath: '/p', p_git_repo: '', p_main_branch: 'main',
        p_git_url: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('project create failed');
    return created.project_id;

}

describe('sp_Milestone_Create', () => {

    it('inserts with default tracking_status=not-started + relevance=active', async () => {

        const id = await makeMilestone('first');

        const row = await ctx.kysely.selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('first');
        expect(row.tracking_status).toBe('not-started');
        expect(row.relevance_status).toBe('active');

    });

});

describe('sp_Milestone_Update', () => {

    it('rewrites title/content/reason but preserves status', async () => {

        const id = await makeMilestone('before');

        await ctx.proc('sp_Milestone_Update', {
            p_milestone_id: id, p_title: 'after', p_content: 'c', p_reason: 'r',
        });

        const row = await ctx.kysely.selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('after');
        expect(row.tracking_status).toBe('not-started');
        expect(row.relevance_status).toBe('active');

    });

});

describe('sp_Milestone_SetTracking', () => {

    it('walks not-started -> in-progress -> done and writes audit rows', async () => {

        const id = await makeMilestone();

        await ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id: id, p_new_tracking_status: 'in-progress',
            p_agent_id: 0, p_reason: 'kicking off',
        });

        await ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id: id, p_new_tracking_status: 'done',
            p_agent_id: 0, p_reason: 'finished',
        });

        const row = await ctx.kysely.selectFrom('Milestone')
            .select('tracking_status')
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.tracking_status).toBe('done');

        const subs = await ctx.kysely.selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', id)
            .execute();

        expect(subs.length).toBe(2);

    });

    it('rejects an unallowed transition (not-started -> done)', async () => {

        const id = await makeMilestone();

        // Proc raises 'transition not-started -> done not allowed for milestone-tracking'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Milestone_SetTracking', {
                p_milestone_id: id, p_new_tracking_status: 'done',
                p_agent_id: 0, p_reason: 'should reject',
            }),
        ).rejects.toThrow(/transition not-started -> done not allowed for milestone-tracking/);

    });

});

describe('sp_Milestone_SetRelevance', () => {

    it('moves active -> needs-review and writes Milestone_StateTransition', async () => {

        const id = await makeMilestone();

        await ctx.proc('sp_Milestone_SetRelevance', {
            p_milestone_id: id, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'review',
        });

        const row = await ctx.kysely.selectFrom('Milestone')
            .select('relevance_status')
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('needs-review');

        const sub = await ctx.kysely.selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('milestone-relevance');
        expect(trans.from_status).toBe('active');
        expect(trans.to_status).toBe('needs-review');

    });

    it('rejects active -> active', async () => {

        const id = await makeMilestone();

        // Proc raises 'transition active -> active not allowed for milestone-relevance'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Milestone_SetRelevance', {
                p_milestone_id: id, p_new_relevance_status: 'active',
                p_agent_id: 0, p_reason: 'reject',
            }),
        ).rejects.toThrow(/transition active -> active not allowed for milestone-relevance/);

    });

});

describe('sp_Milestone_Delete + sp_Milestone_Restore', () => {

    it('round-trips active -> deleted -> active', async () => {

        const id = await makeMilestone();

        await ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: id, p_agent_id: 0, p_reason: 'cleanup',
        });

        const deleted = await ctx.kysely.selectFrom('Milestone')
            .select('relevance_status')
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(deleted.relevance_status).toBe('deleted');

        await ctx.proc('sp_Milestone_Restore', {
            p_milestone_id: id, p_agent_id: 0, p_reason: 'kept',
        });

        const restored = await ctx.kysely.selectFrom('Milestone')
            .select('relevance_status')
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(restored.relevance_status).toBe('active');

    });

});

describe('sp_Milestone_Close', () => {

    it('closes the milestone and abandons all open tasks', async () => {

        const id = await makeMilestone();

        const t1 = await makeTask(id);
        const t2 = await makeTask(id);
        const t3 = await makeTask(id);

        // Move milestone into in-progress so Close's done transition is allowed.
        await ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id: id, p_new_tracking_status: 'in-progress',
            p_agent_id: 0, p_reason: 'kickoff',
        });

        // Drive t3 to done first — should NOT be re-abandoned.
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t3.milestone_id, p_task_no: t3.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: 'work',
        });
        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: t3.milestone_id, p_task_no: t3.task_no,
            p_new_tracking_status: 'done', p_agent_id: 0, p_reason: 'finished',
        });

        await ctx.proc('sp_Milestone_Close', {
            p_milestone_id: id, p_agent_id: 0, p_reason: 'wrap',
        });

        const ms = await ctx.kysely.selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(ms.tracking_status).toBe('done');
        expect(ms.relevance_status).toBe('superseded');

        const t1Row = await ctx.kysely.selectFrom('Task')
            .select('tracking_status')
            .where('milestone_id', '=', t1.milestone_id)
            .where('task_no', '=', t1.task_no)
            .executeTakeFirstOrThrow();
        expect(t1Row.tracking_status).toBe('abandoned');

        const t2Row = await ctx.kysely.selectFrom('Task')
            .select('tracking_status')
            .where('milestone_id', '=', t2.milestone_id)
            .where('task_no', '=', t2.task_no)
            .executeTakeFirstOrThrow();
        expect(t2Row.tracking_status).toBe('abandoned');

        const t3Row = await ctx.kysely.selectFrom('Task')
            .select('tracking_status')
            .where('milestone_id', '=', t3.milestone_id)
            .where('task_no', '=', t3.task_no)
            .executeTakeFirstOrThrow();
        expect(t3Row.tracking_status).toBe('done');

    });

});

describe('sp_Milestone_Attach_Project + sp_Milestone_Detach_Project', () => {

    it('writes and removes the Project_Milestone join row', async () => {

        const milestoneId = await makeMilestone();
        const projectId = await makeProject('p-ms');

        await ctx.proc('sp_Milestone_Attach_Project', {
            p_milestone_id: milestoneId, p_project_id: projectId,
        });

        const join = await ctx.kysely.selectFrom('Project_Milestone')
            .selectAll()
            .where('project_id', '=', projectId)
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Milestone_Detach_Project', {
            p_milestone_id: milestoneId, p_project_id: projectId,
        });

        const gone = await ctx.kysely.selectFrom('Project_Milestone')
            .selectAll()
            .where('project_id', '=', projectId)
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});
