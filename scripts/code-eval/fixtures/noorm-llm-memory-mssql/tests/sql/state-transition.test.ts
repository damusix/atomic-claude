/**
 * Layer 1 cross-cutting tests for the StateTransition audit pipeline.
 *
 * Every state-machine proc — Memory_SetRelevance, Note_SetRelevance,
 * Artifact_SetRelevance, Milestone_SetTracking, Milestone_SetRelevance,
 * Task_SetTracking — must insert exactly one row in StateTransition AND
 * one row in the matching *_StateTransition subtype, with from_status,
 * to_status, and agent_id matching the call. The CHECK constraints on
 * the subtype tables enforce that state_transition_type lines up with
 * the subtype, so a mis-routed audit row would itself fail.
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
        name: `audit-agent-${Date.now()}-${Math.random()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('agent creation failed');
    }

    return row.agent_id;

}

describe('sql.state-transition: subtype + basetype audit pairing', () => {

    it('sp_Memory_SetRelevance writes one StateTransition + one Memory_StateTransition', async () => {

        const agentId = await createAgent();

        const memRows = await ctx.proc('sp_Memory_Create', {
            content: 'audit subject',
            domain: 'backend',
            category: 'fact',
        });

        const memoryId = memRows[0]?.memory_id;

        if (typeof memoryId !== 'number') {

            throw new Error('memory creation failed');
        }

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: memoryId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
            reason: 'memory audit',
        });

        const subtype = await ctx.kysely
            .selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

        expect(subtype.length).toBe(1);

        const transitionId = subtype[0]?.transition_id;

        if (typeof transitionId !== 'number') {

            throw new Error('subtype row missing transition_id');
        }

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', transitionId)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('memory-relevance');
        expect(base?.from_status).toBe('active');
        expect(base?.to_status).toBe('needs-review');
        expect(base?.agent_id).toBe(agentId);

    });

    it('sp_Note_SetRelevance writes one StateTransition + one Note_StateTransition', async () => {

        const agentId = await createAgent();

        const projRows = await ctx.proc('sp_Project_Create', {
            name: `audit-project-${Date.now()}`,
        });

        const projectId = projRows[0]?.project_id;

        if (typeof projectId !== 'number') {

            throw new Error('project creation failed');
        }

        const noteRows = await ctx.proc('sp_Note_Create_Project', {
            content: 'audit note',
            project_id: projectId,
        });

        const noteId = noteRows[0]?.note_id;

        if (typeof noteId !== 'number') {

            throw new Error('note creation failed');
        }

        await ctx.proc('sp_Note_SetRelevance', {
            note_id: noteId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const subtype = await ctx.kysely
            .selectFrom('Note_StateTransition')
            .selectAll()
            .where('note_id', '=', noteId)
            .execute();

        expect(subtype.length).toBe(1);

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype[0]!.transition_id)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('note-relevance');
        expect(base?.from_status).toBe('active');
        expect(base?.to_status).toBe('needs-review');
        expect(base?.agent_id).toBe(agentId);

    });

    it('sp_Artifact_SetRelevance writes one StateTransition + one Artifact_StateTransition', async () => {

        const agentId = await createAgent();

        const artRows = await ctx.proc('sp_Artifact_Create', {
            title: 'audit artifact',
        });

        const artifactId = artRows[0]?.artifact_id;

        if (typeof artifactId !== 'number') {

            throw new Error('artifact creation failed');
        }

        await ctx.proc('sp_Artifact_SetRelevance', {
            artifact_id: artifactId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const subtype = await ctx.kysely
            .selectFrom('Artifact_StateTransition')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .execute();

        expect(subtype.length).toBe(1);

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype[0]!.transition_id)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('artifact-relevance');
        expect(base?.from_status).toBe('active');
        expect(base?.to_status).toBe('needs-review');
        expect(base?.agent_id).toBe(agentId);

    });

    it('sp_Milestone_SetTracking writes one StateTransition + one Milestone_StateTransition (milestone-tracking)', async () => {

        const agentId = await createAgent();

        const mileRows = await ctx.proc('sp_Milestone_Create', {
            title: 'audit milestone tracking',
        });

        const milestoneId = mileRows[0]?.milestone_id;

        if (typeof milestoneId !== 'number') {

            throw new Error('milestone creation failed');
        }

        await ctx.proc('sp_Milestone_SetTracking', {
            milestone_id: milestoneId,
            new_tracking_status: 'in-progress',
            agent_id: agentId,
        });

        const subtype = await ctx.kysely
            .selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(subtype.length).toBe(1);

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype[0]!.transition_id)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('milestone-tracking');
        expect(base?.from_status).toBe('not-started');
        expect(base?.to_status).toBe('in-progress');
        expect(base?.agent_id).toBe(agentId);

    });

    it('sp_Milestone_SetRelevance writes one StateTransition + one Milestone_StateTransition (milestone-relevance)', async () => {

        const agentId = await createAgent();

        const mileRows = await ctx.proc('sp_Milestone_Create', {
            title: 'audit milestone relevance',
        });

        const milestoneId = mileRows[0]?.milestone_id;

        if (typeof milestoneId !== 'number') {

            throw new Error('milestone creation failed');
        }

        await ctx.proc('sp_Milestone_SetRelevance', {
            milestone_id: milestoneId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const subtype = await ctx.kysely
            .selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .execute();

        expect(subtype.length).toBe(1);

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype[0]!.transition_id)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('milestone-relevance');
        expect(base?.from_status).toBe('active');
        expect(base?.to_status).toBe('needs-review');
        expect(base?.agent_id).toBe(agentId);

    });

    it('Memory_StateTransition CHECK rejects a row whose parent StateTransition has the wrong discriminator', async () => {

        const agentId = await createAgent();

        const memRows = await ctx.proc('sp_Memory_Create', {
            content: 'check-constraint subject',
            domain: 'backend',
            category: 'fact',
        });

        const memoryId = memRows[0]?.memory_id;

        if (typeof memoryId !== 'number') {

            throw new Error('memory creation failed');
        }

        // Insert a StateTransition with the WRONG discriminator. The base
        // table accepts it (state_transition_type is FK to a reference
        // table — 'note-relevance' is a valid type), but the subtype CHECK
        // for Memory_StateTransition demands 'memory-relevance'.
        const inserted = await ctx.kysely
            .insertInto('StateTransition')
            .values({
                state_transition_type: 'note-relevance',
                agent_id: agentId,
                from_status: 'active',
                to_status: 'needs-review',
                reason: 'mis-route probe',
            })
            .output('inserted.transition_id')
            .executeTakeFirstOrThrow();

        const transitionId = inserted.transition_id;

        await expect(
            ctx.kysely
                .insertInto('Memory_StateTransition')
                .values({ transition_id: transitionId, memory_id: memoryId })
                .execute(),
        ).rejects.toThrow(/CHECK constraint|CK_Memory_StateTransition_Type/i);

    });

    it('sp_Task_SetTracking writes one StateTransition + one Task_StateTransition (task-tracking)', async () => {

        const agentId = await createAgent();

        const mileRows = await ctx.proc('sp_Milestone_Create', {
            title: 'audit task host',
        });

        const milestoneId = mileRows[0]?.milestone_id;

        if (typeof milestoneId !== 'number') {

            throw new Error('milestone creation failed');
        }

        const taskRows = await ctx.proc('sp_Task_Create', {
            milestone_id: milestoneId,
            title: 'audit task',
        });

        const task = taskRows[0];

        if (!task) {

            throw new Error('task creation failed');
        }

        await ctx.proc('sp_Task_SetTracking', {
            milestone_id: task.milestone_id,
            task_no: task.task_no,
            new_tracking_status: 'in-progress',
            agent_id: agentId,
        });

        const subtype = await ctx.kysely
            .selectFrom('Task_StateTransition')
            .selectAll()
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .execute();

        expect(subtype.length).toBe(1);

        const base = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', subtype[0]!.transition_id)
            .executeTakeFirst();

        expect(base?.state_transition_type).toBe('task-tracking');
        expect(base?.from_status).toBe('not-started');
        expect(base?.to_status).toBe('in-progress');
        expect(base?.agent_id).toBe(agentId);

    });

});
