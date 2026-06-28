/**
 * Layer 1 SQL contract tests for the Artifact domain.
 *
 * Artifact is an elevated entity with a single state machine
 * (relevance only — no tracking_status). It attaches to Milestones and
 * Tasks via two domain-owned binary facts. Every attach proc is
 * idempotent on its composite PK.
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
        name: `art-agent-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('agent creation failed');
    }

    return row.agent_id;

}

async function createArtifact(title = 'an artifact'): Promise<number> {

    const rows = await ctx.proc('sp_Artifact_Create', { title });

    const row = rows[0];

    if (!row) {

        throw new Error('artifact creation failed');
    }

    return row.artifact_id;

}

async function createMilestone(): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', {
        title: `art-milestone-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('milestone creation failed');
    }

    return row.milestone_id;

}

async function createTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const rows = await ctx.proc('sp_Task_Create', {
        milestone_id: milestoneId,
        title: `art-task-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('task creation failed');
    }

    return row;

}

describe('sql.artifact: CRUD + state machine', () => {

    it('sp_Artifact_Create returns positive id and persists with active relevance', async () => {

        const id = await createArtifact('first artifact');

        expect(id).toBeGreaterThan(0);

        const row = await ctx.kysely
            .selectFrom('Artifact')
            .selectAll()
            .where('artifact_id', '=', id)
            .executeTakeFirst();

        expect(row?.title).toBe('first artifact');
        expect(row?.relevance_status).toBe('active');

    });

    it('sp_Artifact_Update mutates title/description/filepath observably', async () => {

        const id = await createArtifact();

        await ctx.proc('sp_Artifact_Update', {
            artifact_id: id,
            title: 'updated title',
            description: 'updated description',
            filepath: '/tmp/updated.txt',
        });

        const row = await ctx.kysely
            .selectFrom('Artifact')
            .select(['title', 'description', 'filepath'])
            .where('artifact_id', '=', id)
            .executeTakeFirst();

        expect(row?.title).toBe('updated title');
        expect(row?.description).toBe('updated description');
        expect(row?.filepath).toBe('/tmp/updated.txt');

    });

    it('sp_Artifact_SetRelevance moves active -> needs-review and writes audit row', async () => {

        const agentId = await createAgent();
        const id = await createArtifact();

        await ctx.proc('sp_Artifact_SetRelevance', {
            artifact_id: id,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const row = await ctx.kysely
            .selectFrom('Artifact')
            .select(['relevance_status'])
            .where('artifact_id', '=', id)
            .executeTakeFirst();

        expect(row?.relevance_status).toBe('needs-review');

        const audit = await ctx.kysely
            .selectFrom('Artifact_StateTransition as ast')
            .innerJoin('StateTransition as st', 'st.transition_id', 'ast.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.state_transition_type'])
            .where('ast.artifact_id', '=', id)
            .executeTakeFirst();

        expect(audit?.from_status).toBe('active');
        expect(audit?.to_status).toBe('needs-review');
        expect(audit?.state_transition_type).toBe('artifact-relevance');

    });

    it('sp_Artifact_SetRelevance rejects an illegal transition (active -> active)', async () => {

        const agentId = await createAgent();
        const id = await createArtifact();

        await expect(
            ctx.proc('sp_Artifact_SetRelevance', {
                artifact_id: id,
                new_relevance_status: 'active',
                agent_id: agentId,
            }),
        ).rejects.toThrow(/Relevance transition not allowed/i);

    });

    it('sp_Artifact_Delete + sp_Artifact_Restore round-trip flips relevance back to active', async () => {

        const agentId = await createAgent();
        const id = await createArtifact();

        await ctx.proc('sp_Artifact_Delete', {
            artifact_id: id,
            agent_id: agentId,
        });

        const deleted = await ctx.kysely
            .selectFrom('Artifact')
            .select(['relevance_status'])
            .where('artifact_id', '=', id)
            .executeTakeFirst();

        expect(deleted?.relevance_status).toBe('deleted');

        await ctx.proc('sp_Artifact_Restore', {
            artifact_id: id,
            agent_id: agentId,
        });

        const restored = await ctx.kysely
            .selectFrom('Artifact')
            .select(['relevance_status'])
            .where('artifact_id', '=', id)
            .executeTakeFirst();

        expect(restored?.relevance_status).toBe('active');

    });

});

describe('sql.artifact: attach/detach idempotency', () => {

    it('sp_Artifact_Attach_Milestone / Detach_Milestone are idempotent', async () => {

        const artifactId = await createArtifact();
        const milestoneId = await createMilestone();

        await ctx.proc('sp_Artifact_Attach_Milestone', {
            artifact_id: artifactId,
            milestone_id: milestoneId,
        });
        await ctx.proc('sp_Artifact_Attach_Milestone', {
            artifact_id: artifactId,
            milestone_id: milestoneId,
        });

        const attached = await ctx.kysely
            .selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Artifact_Detach_Milestone', {
            artifact_id: artifactId,
            milestone_id: milestoneId,
        });
        await ctx.proc('sp_Artifact_Detach_Milestone', {
            artifact_id: artifactId,
            milestone_id: milestoneId,
        });

        const detached = await ctx.kysely
            .selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(detached.length).toBe(0);

    });

    it('sp_Artifact_Attach_Task / Detach_Task are idempotent (composite Task PK)', async () => {

        const artifactId = await createArtifact();
        const milestoneId = await createMilestone();
        const task = await createTask(milestoneId);

        await ctx.proc('sp_Artifact_Attach_Task', {
            artifact_id: artifactId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });
        await ctx.proc('sp_Artifact_Attach_Task', {
            artifact_id: artifactId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const attached = await ctx.kysely
            .selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Artifact_Detach_Task', {
            artifact_id: artifactId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });
        await ctx.proc('sp_Artifact_Detach_Task', {
            artifact_id: artifactId,
            milestone_id: task.milestone_id,
            task_no: task.task_no,
        });

        const detached = await ctx.kysely
            .selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(detached.length).toBe(0);

    });

});
