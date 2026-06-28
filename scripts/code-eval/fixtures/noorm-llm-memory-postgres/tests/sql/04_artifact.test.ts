import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeArtifact(title = 'doc.md'): Promise<number> {

    const result = await ctx.proc('sp_Artifact_Create', {
        p_title: title, p_description: '', p_filepath: title,
        p_reason: 'fixture', p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('artifact create failed');
    return created.artifact_id;

}

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

describe('sp_Artifact_Create', () => {

    it('inserts an active artifact and returns the new id', async () => {

        const id = await makeArtifact('design.md');

        const row = await ctx.kysely.selectFrom('Artifact')
            .selectAll()
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('design.md');
        expect(row.relevance_status).toBe('active');

    });

});

describe('sp_Artifact_Update', () => {

    it('rewrites title/description/filepath/reason', async () => {

        const id = await makeArtifact('before.md');

        await ctx.proc('sp_Artifact_Update', {
            p_artifact_id: id, p_title: 'after.md', p_description: 'd',
            p_filepath: 'docs/after.md', p_reason: 'renamed',
        });

        const row = await ctx.kysely.selectFrom('Artifact')
            .selectAll()
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.title).toBe('after.md');
        expect(row.filepath).toBe('docs/after.md');
        expect(row.relevance_status).toBe('active');

    });

});

describe('sp_Artifact_SetRelevance', () => {

    it('moves active -> needs-review and writes Artifact_StateTransition', async () => {

        const id = await makeArtifact();

        await ctx.proc('sp_Artifact_SetRelevance', {
            p_artifact_id: id, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'review',
        });

        const row = await ctx.kysely.selectFrom('Artifact')
            .select('relevance_status')
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(row.relevance_status).toBe('needs-review');

        const sub = await ctx.kysely.selectFrom('Artifact_StateTransition')
            .selectAll()
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('artifact-relevance');
        expect(trans.from_status).toBe('active');
        expect(trans.to_status).toBe('needs-review');

    });

    it('rejects an unallowed transition (active -> active)', async () => {

        const id = await makeArtifact();

        // Proc raises 'transition active -> active not allowed for artifact-relevance'
        // with SQLSTATE '23514' (check_violation).
        await expect(
            ctx.proc('sp_Artifact_SetRelevance', {
                p_artifact_id: id, p_new_relevance_status: 'active',
                p_agent_id: 0, p_reason: 'should reject',
            }),
        ).rejects.toThrow(/transition active -> active not allowed for artifact-relevance/);

    });

});

describe('sp_Artifact_Delete + sp_Artifact_Restore', () => {

    it('round-trips active -> deleted -> active', async () => {

        const id = await makeArtifact();

        await ctx.proc('sp_Artifact_Delete', {
            p_artifact_id: id, p_agent_id: 0, p_reason: 'cleanup',
        });

        const deleted = await ctx.kysely.selectFrom('Artifact')
            .select('relevance_status')
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(deleted.relevance_status).toBe('deleted');

        await ctx.proc('sp_Artifact_Restore', {
            p_artifact_id: id, p_agent_id: 0, p_reason: 'kept',
        });

        const restored = await ctx.kysely.selectFrom('Artifact')
            .select('relevance_status')
            .where('artifact_id', '=', id)
            .executeTakeFirstOrThrow();

        expect(restored.relevance_status).toBe('active');

    });

});

describe('sp_Artifact_Attach_Milestone + sp_Artifact_Detach_Milestone', () => {

    it('writes and removes the Milestone_Artifact join row', async () => {

        const artifactId = await makeArtifact();
        const milestoneId = await makeMilestone();

        await ctx.proc('sp_Artifact_Attach_Milestone', {
            p_artifact_id: artifactId, p_milestone_id: milestoneId,
        });

        const join = await ctx.kysely.selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Artifact_Detach_Milestone', {
            p_artifact_id: artifactId, p_milestone_id: milestoneId,
        });

        const gone = await ctx.kysely.selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});

describe('sp_Artifact_Attach_Task + sp_Artifact_Detach_Task', () => {

    it('writes and removes the Task_Artifact join row', async () => {

        const artifactId = await makeArtifact();
        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        await ctx.proc('sp_Artifact_Attach_Task', {
            p_artifact_id: artifactId,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        const join = await ctx.kysely.selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirst();

        expect(join).toBeDefined();

        await ctx.proc('sp_Artifact_Detach_Task', {
            p_artifact_id: artifactId,
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
        });

        const gone = await ctx.kysely.selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();

        expect(gone).toBeUndefined();

    });

});
