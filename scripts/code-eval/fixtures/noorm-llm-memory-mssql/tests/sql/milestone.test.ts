/**
 * Layer 1 SQL contract tests for the Milestone domain.
 *
 * Milestone is the only elevated entity with TWO state machines —
 * tracking_status (lifecycle) and relevance_status (soft delete) —
 * both auditing through Milestone_StateTransition. sp_Milestone_Close
 * orchestrates done + superseded + abandoning every open child Task,
 * which is the cascade we exercise here.
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
        name: `mile-agent-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('agent creation failed');
    }

    return row.agent_id;

}

async function createMilestone(title = 'a milestone'): Promise<number> {

    const rows = await ctx.proc('sp_Milestone_Create', { title });

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

describe('sql.milestone: CRUD', () => {

    it('sp_Milestone_Create returns positive id with default not-started/active', async () => {

        const id = await createMilestone('first milestone');

        expect(id).toBeGreaterThan(0);

        const row = await ctx.kysely
            .selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(row?.title).toBe('first milestone');
        expect(row?.tracking_status).toBe('not-started');
        expect(row?.relevance_status).toBe('active');

    });

    it('sp_Milestone_Update mutates title/content/reason observably', async () => {

        const id = await createMilestone();

        await ctx.proc('sp_Milestone_Update', {
            milestone_id: id,
            title: 'new title',
            content: 'new content',
            reason: 'because',
        });

        const row = await ctx.kysely
            .selectFrom('Milestone')
            .select(['title', 'content', 'reason'])
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(row?.title).toBe('new title');
        expect(row?.content).toBe('new content');
        expect(row?.reason).toBe('because');

    });

});

describe('sql.milestone: tracking state machine', () => {

    it('sp_Milestone_SetTracking moves not-started -> in-progress and audits the transition', async () => {

        const agentId = await createAgent();
        const id = await createMilestone();

        await ctx.proc('sp_Milestone_SetTracking', {
            milestone_id: id,
            new_tracking_status: 'in-progress',
            agent_id: agentId,
        });

        const row = await ctx.kysely
            .selectFrom('Milestone')
            .select(['tracking_status'])
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(row?.tracking_status).toBe('in-progress');

        const audit = await ctx.kysely
            .selectFrom('Milestone_StateTransition as mst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'mst.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.state_transition_type'])
            .where('mst.milestone_id', '=', id)
            .executeTakeFirst();

        expect(audit?.from_status).toBe('not-started');
        expect(audit?.to_status).toBe('in-progress');
        expect(audit?.state_transition_type).toBe('milestone-tracking');

    });

    it('sp_Milestone_SetTracking rejects an illegal transition (not-started -> done)', async () => {

        const agentId = await createAgent();
        const id = await createMilestone();

        await expect(
            ctx.proc('sp_Milestone_SetTracking', {
                milestone_id: id,
                new_tracking_status: 'done',
                agent_id: agentId,
            }),
        ).rejects.toThrow(/Tracking transition not allowed/i);

    });

});

describe('sql.milestone: relevance state machine', () => {

    it('sp_Milestone_SetRelevance moves active -> needs-review with audit row', async () => {

        const agentId = await createAgent();
        const id = await createMilestone();

        await ctx.proc('sp_Milestone_SetRelevance', {
            milestone_id: id,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const row = await ctx.kysely
            .selectFrom('Milestone')
            .select(['relevance_status'])
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(row?.relevance_status).toBe('needs-review');

        const audit = await ctx.kysely
            .selectFrom('Milestone_StateTransition as mst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'mst.transition_id')
            .select(['st.state_transition_type', 'st.from_status', 'st.to_status'])
            .where('mst.milestone_id', '=', id)
            .where('st.state_transition_type', '=', 'milestone-relevance')
            .executeTakeFirst();

        expect(audit?.from_status).toBe('active');
        expect(audit?.to_status).toBe('needs-review');

    });

    it('sp_Milestone_SetRelevance rejects an illegal transition (active -> active)', async () => {

        const agentId = await createAgent();
        const id = await createMilestone();

        await expect(
            ctx.proc('sp_Milestone_SetRelevance', {
                milestone_id: id,
                new_relevance_status: 'active',
                agent_id: agentId,
            }),
        ).rejects.toThrow(/Relevance transition not allowed/i);

    });

});

describe('sql.milestone: delete + restore', () => {

    it('sp_Milestone_Restore flips relevance_status from deleted back to active', async () => {

        const agentId = await createAgent();
        const id = await createMilestone('to be restored');

        await ctx.proc('sp_Milestone_Delete', {
            milestone_id: id,
            agent_id: agentId,
        });

        const deleted = await ctx.kysely
            .selectFrom('Milestone')
            .select(['relevance_status'])
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(deleted?.relevance_status).toBe('deleted');

        await ctx.proc('sp_Milestone_Restore', {
            milestone_id: id,
            agent_id: agentId,
        });

        const restored = await ctx.kysely
            .selectFrom('Milestone')
            .select(['relevance_status'])
            .where('milestone_id', '=', id)
            .executeTakeFirst();

        expect(restored?.relevance_status).toBe('active');

        const transitions = await ctx.kysely
            .selectFrom('Milestone_StateTransition as mst')
            .innerJoin('StateTransition as st', 'st.transition_id', 'mst.transition_id')
            .select(['st.from_status', 'st.to_status'])
            .where('mst.milestone_id', '=', id)
            .where('st.state_transition_type', '=', 'milestone-relevance')
            .orderBy('st.transition_id')
            .execute();

        expect(transitions.length).toBe(2);
        expect(transitions[0]?.to_status).toBe('deleted');
        expect(transitions[1]?.from_status).toBe('deleted');
        expect(transitions[1]?.to_status).toBe('active');

    });

});

describe('sql.milestone: close cascade', () => {

    it('sp_Milestone_Close abandons every open child task', async () => {

        const agentId = await createAgent();
        const milestoneId = await createMilestone('to be closed');
        const taskA = await createTask(milestoneId, 'open task A');
        const taskB = await createTask(milestoneId, 'open task B');

        // Move milestone into in-progress so the close transition is legal
        // (not-started -> done is NOT allowed by the seed whitelist).
        await ctx.proc('sp_Milestone_SetTracking', {
            milestone_id: milestoneId,
            new_tracking_status: 'in-progress',
            agent_id: agentId,
        });

        // Both child tasks are still 'not-started' (open).
        await ctx.proc('sp_Milestone_Close', {
            milestone_id: milestoneId,
            agent_id: agentId,
            reason: 'closing milestone for test',
        });

        const milestone = await ctx.kysely
            .selectFrom('Milestone')
            .select(['tracking_status', 'relevance_status'])
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

        expect(milestone?.tracking_status).toBe('done');
        expect(milestone?.relevance_status).toBe('superseded');

        const tasks = await ctx.kysely
            .selectFrom('Task')
            .select(['task_no', 'tracking_status'])
            .where('milestone_id', '=', milestoneId)
            .orderBy('task_no')
            .execute();

        expect(tasks.length).toBe(2);
        expect(tasks[0]?.task_no).toBe(taskA.task_no);
        expect(tasks[0]?.tracking_status).toBe('abandoned');
        expect(tasks[1]?.task_no).toBe(taskB.task_no);
        expect(tasks[1]?.tracking_status).toBe('abandoned');

    });

});
