import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function makeMemory(): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content: 'm', p_domain: 'backend', p_category: 'fact',
        p_reason: '', p_provenance_id: 0, p_agent_id: 0,
        p_was_inferred: false, p_was_observed: true,
        p_was_evidenced: false, p_was_user_provided: false,
    });
    const [created] = result;
    if (!created) throw new Error('memory create failed');
    return created.memory_id;

}

async function makeNote(): Promise<number> {

    const result = await ctx.proc('sp_Note_Create_Project', {
        p_content: 'n', p_reason: '', p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('note create failed');
    return created.note_id;

}

async function makeArtifact(): Promise<number> {

    const result = await ctx.proc('sp_Artifact_Create', {
        p_title: 'a', p_description: '', p_filepath: 'a',
        p_reason: '', p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('artifact create failed');
    return created.artifact_id;

}

async function makeMilestone(): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: 'M', p_content: '', p_reason: '',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('milestone create failed');
    return created.milestone_id;

}

async function makeTask(milestoneId: number): Promise<{ milestone_id: number; task_no: number }> {

    const result = await ctx.proc('sp_Task_Create', {
        p_milestone_id: milestoneId, p_title: 'T', p_content: '',
        p_reason: '', p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('task create failed');
    return created;

}

describe('memory-relevance state transitions', () => {

    it('SetRelevance writes a paired Memory_StateTransition + StateTransition row', async () => {

        const memoryId = await makeMemory();

        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id: memoryId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'audit',
        });

        const sub = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('memory-relevance');
        expect(trans.from_status).toBe('active');
        expect(trans.to_status).toBe('needs-review');

    });

    it('Delete + Restore each write their own audit row', async () => {

        const memoryId = await makeMemory();

        await ctx.proc('sp_Memory_Delete', { p_memory_id: memoryId, p_agent_id: 0, p_reason: 'd' });
        await ctx.proc('sp_Memory_Restore', { p_memory_id: memoryId, p_agent_id: 0, p_reason: 'r' });

        const subs = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

        expect(subs.length).toBe(2);

    });

});

describe('note-relevance state transitions', () => {

    it('SetRelevance writes a paired Note_StateTransition + StateTransition row', async () => {

        const noteId = await makeNote();

        await ctx.proc('sp_Note_SetRelevance', {
            p_note_id: noteId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const sub = await ctx.kysely.selectFrom('Note_StateTransition')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('note-relevance');
        expect(trans.from_status).toBe('active');
        expect(trans.to_status).toBe('needs-review');

    });

});

describe('artifact-relevance state transitions', () => {

    it('SetRelevance writes a paired Artifact_StateTransition + StateTransition row', async () => {

        const artifactId = await makeArtifact();

        await ctx.proc('sp_Artifact_SetRelevance', {
            p_artifact_id: artifactId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const sub = await ctx.kysely.selectFrom('Artifact_StateTransition')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('artifact-relevance');
        expect(trans.from_status).toBe('active');
        expect(trans.to_status).toBe('needs-review');

    });

});

describe('milestone-tracking + milestone-relevance state transitions', () => {

    it('SetTracking writes audit with type milestone-tracking', async () => {

        const id = await makeMilestone();

        await ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id: id, p_new_tracking_status: 'in-progress',
            p_agent_id: 0, p_reason: 'go',
        });

        const sub = await ctx.kysely.selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', id)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('milestone-tracking');
        expect(trans.from_status).toBe('not-started');
        expect(trans.to_status).toBe('in-progress');

    });

    it('Delete writes audit with type milestone-relevance', async () => {

        const id = await makeMilestone();

        await ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: id, p_agent_id: 0, p_reason: 'd',
        });

        const subs = await ctx.kysely.selectFrom('Milestone_StateTransition')
            .selectAll()
            .where('milestone_id', '=', id)
            .execute();

        const transIds = subs.map((s) => s.transition_id);

        const transitions = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', 'in', transIds)
            .execute();

        const types = new Set(transitions.map((t) => t.state_transition_type));
        expect(types.has('milestone-relevance')).toBe(true);

    });

});

describe('task-tracking state transitions', () => {

    it('SetTracking writes audit with type task-tracking', async () => {

        const milestoneId = await makeMilestone();
        const task = await makeTask(milestoneId);

        await ctx.proc('sp_Task_SetTracking', {
            p_milestone_id: task.milestone_id, p_task_no: task.task_no,
            p_new_tracking_status: 'in-progress', p_agent_id: 0, p_reason: 'go',
        });

        const sub = await ctx.kysely.selectFrom('Task_StateTransition')
            .selectAll()
            .where('milestone_id', '=', task.milestone_id)
            .where('task_no', '=', task.task_no)
            .executeTakeFirstOrThrow();

        const trans = await ctx.kysely.selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', '=', sub.transition_id)
            .executeTakeFirstOrThrow();

        expect(trans.state_transition_type).toBe('task-tracking');
        expect(trans.from_status).toBe('not-started');
        expect(trans.to_status).toBe('in-progress');

    });

});

describe('StateTransition exclusivity trigger', () => {

    it('rejects writing a memory-relevance transition into Note_StateTransition', async () => {

        const memoryId = await makeMemory();

        // Create a memory-relevance transition via the proc.
        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id: memoryId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const sub = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        // Now attempt to also write the same transition_id into Note_StateTransition.
        // The trigger must reject because state_transition_type='memory-relevance'
        // is incompatible with Note_StateTransition (which only allows note-relevance).
        await ctx.kysely.insertInto('Note')
            .values({
                note_type: 'project', relevance_status: 'active',
                provenance_id: 0, agent_id: 0,
                content: 'guard', reason: 'guard',
            })
            .execute();

        const note = await ctx.kysely.selectFrom('Note')
            .select('note_id')
            .orderBy('note_id', 'desc')
            .limit(1)
            .executeTakeFirstOrThrow();

        // Trigger raises 'trg_check_state_transition_exclusivity: transition_id N has
        // state_transition_type=memory-relevance but is being written into
        // Note_StateTransition (allowed: {note-relevance})'.
        await expect(
            ctx.kysely.insertInto('Note_StateTransition')
                .values({ transition_id: sub.transition_id, note_id: note.note_id })
                .execute(),
        ).rejects.toThrow(/trg_check_state_transition_exclusivity.*state_transition_type=memory-relevance.*Note_StateTransition/);

    });

    it('rejects writing a note-relevance transition into Memory_StateTransition', async () => {

        const noteId = await makeNote();

        await ctx.proc('sp_Note_SetRelevance', {
            p_note_id: noteId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const sub = await ctx.kysely.selectFrom('Note_StateTransition')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirstOrThrow();

        const memoryId = await makeMemory();

        // Trigger raises 'trg_check_state_transition_exclusivity: transition_id N has
        // state_transition_type=note-relevance but is being written into
        // Memory_StateTransition (allowed: {memory-relevance})'.
        await expect(
            ctx.kysely.insertInto('Memory_StateTransition')
                .values({ transition_id: sub.transition_id, memory_id: memoryId })
                .execute(),
        ).rejects.toThrow(/trg_check_state_transition_exclusivity.*state_transition_type=note-relevance.*Memory_StateTransition/);

    });

    it('rejects updating StateTransition.state_transition_type to a value incompatible with the holding subtype', async () => {

        const memoryId = await makeMemory();

        await ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id: memoryId, p_new_relevance_status: 'needs-review',
            p_agent_id: 0, p_reason: 'flag',
        });

        const sub = await ctx.kysely.selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        // The basetype's state_transition_type is locked to 'memory-relevance' as long
        // as Memory_StateTransition holds the row. Attempting to change it raises
        // 'trg_check_state_transition_type_update: transition_id N is held in
        // Memory_StateTransition (allowed types: {memory-relevance}) but
        // state_transition_type was changed to note-relevance'.
        await expect(
            ctx.kysely.updateTable('StateTransition')
                .set({ state_transition_type: 'note-relevance' })
                .where('transition_id', '=', sub.transition_id)
                .execute(),
        ).rejects.toThrow(/trg_check_state_transition_type_update.*Memory_StateTransition.*note-relevance/);

    });

});
