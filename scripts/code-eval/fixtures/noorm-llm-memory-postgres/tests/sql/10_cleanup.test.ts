import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';
import { sql } from 'kysely';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

/**
 * sp_Cleanup is a plain CALLable procedure that the project's generated
 * `Procs` catalog does not yet include, so we invoke it via the raw
 * Kysely `sql` tag rather than `ctx.proc('sp_Cleanup', …)`.
 *
 * Once the catalog regenerates with the cleanup proc, callers can be
 * migrated to `ctx.proc('sp_Cleanup', { p_ttl_days })`.
 */
async function callCleanup(ttlDays: number): Promise<void> {

    await sql`CALL "sp_Cleanup"(${ttlDays})`.execute(ctx.kysely);

}

async function createMemory(content = 'baseline'): Promise<number> {

    const result = await ctx.proc('sp_Memory_Create', {
        p_content:           content,
        p_domain:            'backend',
        p_category:          'fact',
        p_reason:            'cleanup fixture',
        p_provenance_id:     0,
        p_agent_id:          0,
        p_was_inferred:      false,
        p_was_observed:      true,
        p_was_evidenced:     false,
        p_was_user_provided: false,
    });
    const [created] = result;
    if (!created) throw new Error('sp_Memory_Create returned no rows');
    return created.memory_id;

}

async function createNote(): Promise<number> {

    const result = await ctx.proc('sp_Note_Create_Project', {
        p_content: 'cleanup-note', p_reason: 'fixture',
        p_provenance_id: 0, p_agent_id: 0, p_project_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('sp_Note_Create_Project returned no rows');
    return created.note_id;

}

async function createArtifact(): Promise<number> {

    const result = await ctx.proc('sp_Artifact_Create', {
        p_title: 'cleanup-art', p_description: '', p_filepath: '/tmp/x.txt',
        p_reason: 'fixture', p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('sp_Artifact_Create returned no rows');
    return created.artifact_id;

}

async function createMilestone(): Promise<number> {

    const result = await ctx.proc('sp_Milestone_Create', {
        p_title: 'cleanup-ms', p_content: '', p_reason: 'fixture',
        p_provenance_id: 0, p_agent_id: 0,
    });
    const [created] = result;
    if (!created) throw new Error('sp_Milestone_Create returned no rows');
    return created.milestone_id;

}

/**
 * Backdates `updated_at` so sp_Cleanup's TTL cutoff treats the row as
 * expired without us actually waiting in real time.
 */
async function backdateMemory(memoryId: number, days: number): Promise<void> {

    await ctx.kysely
        .updateTable('Memory')
        .set({ updated_at: sql<Date>`now() - (${days} || ' days')::interval` })
        .where('memory_id', '=', memoryId)
        .execute();

}

async function backdateNote(noteId: number, days: number): Promise<void> {

    await ctx.kysely
        .updateTable('Note')
        .set({ updated_at: sql<Date>`now() - (${days} || ' days')::interval` })
        .where('note_id', '=', noteId)
        .execute();

}

async function backdateArtifact(artifactId: number, days: number): Promise<void> {

    await ctx.kysely
        .updateTable('Artifact')
        .set({ updated_at: sql<Date>`now() - (${days} || ' days')::interval` })
        .where('artifact_id', '=', artifactId)
        .execute();

}

async function backdateMilestone(milestoneId: number, days: number): Promise<void> {

    await ctx.kysely
        .updateTable('Milestone')
        .set({ updated_at: sql<Date>`now() - (${days} || ' days')::interval` })
        .where('milestone_id', '=', milestoneId)
        .execute();

}

describe('sp_Cleanup', () => {

    it('hard-deletes a soft-deleted Memory whose updated_at is older than ttl_days', async () => {

        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_Delete', {
            p_memory_id: memoryId, p_agent_id: 0, p_reason: 'expired',
        });

        const transitionRows = await ctx.kysely
            .selectFrom('Memory_StateTransition')
            .select('transition_id')
            .where('memory_id', '=', memoryId)
            .execute();

        expect(transitionRows.length).toBeGreaterThan(0);

        const transitionIds = transitionRows.map((r) => r.transition_id);

        await backdateMemory(memoryId, 2);

        await callCleanup(1);

        const memory = await ctx.kysely.selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(memory).toBeUndefined();

        const orphanedSubtypes = await ctx.kysely
            .selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

        expect(orphanedSubtypes.length).toBe(0);

        const orphanedBasetypes = await ctx.kysely
            .selectFrom('StateTransition')
            .selectAll()
            .where('transition_id', 'in', transitionIds)
            .execute();

        expect(orphanedBasetypes.length).toBe(0);

    });

    it('preserves a recent soft-deleted Memory when ttl_days has not elapsed', async () => {

        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_Delete', {
            p_memory_id: memoryId, p_agent_id: 0, p_reason: 'just deleted',
        });

        await callCleanup(7);

        const memory = await ctx.kysely.selectFrom('Memory')
            .select(['memory_id', 'relevance_status'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(memory.relevance_status).toBe('deleted');

    });

    it('preserves active rows even when ttl_days = 0', async () => {

        const memoryId = await createMemory();

        await callCleanup(0);

        const memory = await ctx.kysely.selectFrom('Memory')
            .select(['memory_id', 'relevance_status'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirstOrThrow();

        expect(memory.relevance_status).toBe('active');

    });

    it('hard-deletes soft-deleted Memory, Note, Artifact, and Milestone in one pass', async () => {

        const memoryId    = await createMemory('multi');
        const noteId      = await createNote();
        const artifactId  = await createArtifact();
        const milestoneId = await createMilestone();

        await ctx.proc('sp_Memory_Delete',    { p_memory_id:    memoryId,    p_agent_id: 0, p_reason: 'x' });
        await ctx.proc('sp_Note_Delete',      { p_note_id:      noteId,      p_agent_id: 0, p_reason: 'x' });
        await ctx.proc('sp_Artifact_Delete',  { p_artifact_id:  artifactId,  p_agent_id: 0, p_reason: 'x' });
        await ctx.proc('sp_Milestone_Delete', { p_milestone_id: milestoneId, p_agent_id: 0, p_reason: 'x' });

        await backdateMemory(memoryId, 3);
        await backdateNote(noteId, 3);
        await backdateArtifact(artifactId, 3);
        await backdateMilestone(milestoneId, 3);

        await callCleanup(1);

        const memory = await ctx.kysely.selectFrom('Memory')
            .selectAll().where('memory_id', '=', memoryId).executeTakeFirst();
        const note = await ctx.kysely.selectFrom('Note')
            .selectAll().where('note_id', '=', noteId).executeTakeFirst();
        const artifact = await ctx.kysely.selectFrom('Artifact')
            .selectAll().where('artifact_id', '=', artifactId).executeTakeFirst();
        const milestone = await ctx.kysely.selectFrom('Milestone')
            .selectAll().where('milestone_id', '=', milestoneId).executeTakeFirst();

        expect(memory).toBeUndefined();
        expect(note).toBeUndefined();
        expect(artifact).toBeUndefined();
        expect(milestone).toBeUndefined();

    });

    it('does not touch a different Memory whose soft-delete is still inside the TTL window', async () => {

        const expiredId = await createMemory('expired');
        const freshId   = await createMemory('fresh');

        await ctx.proc('sp_Memory_Delete', { p_memory_id: expiredId, p_agent_id: 0, p_reason: 'old' });
        await ctx.proc('sp_Memory_Delete', { p_memory_id: freshId,   p_agent_id: 0, p_reason: 'new' });

        await backdateMemory(expiredId, 5);

        await callCleanup(2);

        const expired = await ctx.kysely.selectFrom('Memory')
            .selectAll().where('memory_id', '=', expiredId).executeTakeFirst();
        const fresh = await ctx.kysely.selectFrom('Memory')
            .select(['memory_id', 'relevance_status'])
            .where('memory_id', '=', freshId)
            .executeTakeFirstOrThrow();

        expect(expired).toBeUndefined();
        expect(fresh.relevance_status).toBe('deleted');

    });

});
