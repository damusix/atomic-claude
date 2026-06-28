/**
 * Layer 1 SQL contract tests for the Memory domain.
 *
 * Hits every Memory stored procedure end-to-end against a real MSSQL
 * instance through `ctx.proc/kysely` — no Zod, no facade, no mocks.
 * Each test reads state back through Kysely so we assert on observable
 * row changes, not just "the call didn't throw". State-machine
 * rejections are matched against the SQL RAISERROR text.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import { tvp } from '@noormdev/sdk';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];

beforeAll(async () => {

    ({ ctx } = await bootstrap());

});



beforeEach(async () => {

    await resetApplicationData(ctx);

});

async function createAgent(suffix: string): Promise<number> {

    const rows = await ctx.proc('sp_Agent_Create', {
        name: `mem-agent-${suffix}-${Date.now()}`,
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Agent_Create returned no rows');
    }

    return row.agent_id;

}

async function createMemory(content = 'test memory'): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content,
        domain: 'backend',
        category: 'fact',
    });

    const row = rows[0];

    if (!row) {

        throw new Error('sp_Memory_Create returned no rows');
    }

    return row.memory_id;

}

describe('sql.memory: lifecycle', () => {

    it('sp_Memory_Create returns a positive memory_id and persists the row', async () => {

        const memoryId = await createMemory('first memory');

        expect(memoryId).toBeGreaterThan(0);

        const row = await ctx.kysely
            .selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(row).toBeDefined();
        expect(row?.content).toBe('first memory');
        expect(row?.relevance_status).toBe('active');
        expect(row?.access_count).toBe(0);

    });

    it('sp_Memory_Update mutates content/category and is observable via Kysely', async () => {

        const memoryId = await createMemory('original content');

        await ctx.proc('sp_Memory_Update', {
            memory_id: memoryId,
            content: 'updated content',
            domain: 'frontend',
            category: 'pattern',
            was_inferred: true,
        });

        const row = await ctx.kysely
            .selectFrom('Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(row?.content).toBe('updated content');
        expect(row?.domain).toBe('frontend');
        expect(row?.category).toBe('pattern');
        expect(row?.was_inferred).toBe(true);

    });

});

describe('sql.memory: state machine', () => {

    it('sp_Memory_SetRelevance moves active -> needs-review and writes a StateTransition row', async () => {

        const agentId = await createAgent('relevance-ok');
        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_SetRelevance', {
            memory_id: memoryId,
            new_relevance_status: 'needs-review',
            agent_id: agentId,
        });

        const memory = await ctx.kysely
            .selectFrom('Memory')
            .select(['relevance_status'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(memory?.relevance_status).toBe('needs-review');

        const transition = await ctx.kysely
            .selectFrom('StateTransition as st')
            .innerJoin('Memory_StateTransition as mst', 'mst.transition_id', 'st.transition_id')
            .select(['st.from_status', 'st.to_status', 'st.agent_id'])
            .where('mst.memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(transition?.from_status).toBe('active');
        expect(transition?.to_status).toBe('needs-review');
        expect(transition?.agent_id).toBe(agentId);

    });

    it('sp_Memory_SetRelevance rejects an illegal transition (active -> active)', async () => {

        const agentId = await createAgent('relevance-bad');
        const memoryId = await createMemory();

        await expect(
            ctx.proc('sp_Memory_SetRelevance', {
                memory_id: memoryId,
                new_relevance_status: 'active',
                agent_id: agentId,
            }),
        ).rejects.toThrow(/Relevance transition not allowed/i);

    });

    it('sp_Memory_Delete + sp_Memory_Restore round-trip flips relevance back to active', async () => {

        const agentId = await createAgent('delete-restore');
        const memoryId = await createMemory();

        await ctx.proc('sp_Memory_Delete', {
            memory_id: memoryId,
            agent_id: agentId,
        });

        const deleted = await ctx.kysely
            .selectFrom('Memory')
            .select(['relevance_status'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(deleted?.relevance_status).toBe('deleted');

        await ctx.proc('sp_Memory_Restore', {
            memory_id: memoryId,
            agent_id: agentId,
        });

        const restored = await ctx.kysely
            .selectFrom('Memory')
            .select(['relevance_status'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        expect(restored?.relevance_status).toBe('active');

        const transitions = await ctx.kysely
            .selectFrom('Memory_StateTransition')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

        expect(transitions.length).toBe(2);

    });

});

describe('sql.memory: touch', () => {

    it('sp_Memory_Touch increments access_count and bumps last_accessed_at without changing updated_at', async () => {

        const memoryId = await createMemory();

        const before = await ctx.kysely
            .selectFrom('Memory')
            .select(['access_count', 'last_accessed_at', 'updated_at'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        if (!before) {

            throw new Error('memory not found before touch');
        }

        // SYSUTCDATETIME has sub-microsecond resolution; pause to ensure
        // a measurable delta on last_accessed_at after the bump.
        await new Promise((resolve) => setTimeout(resolve, 25));

        await ctx.proc('sp_Memory_Touch', { memory_id: memoryId });

        const after = await ctx.kysely
            .selectFrom('Memory')
            .select(['access_count', 'last_accessed_at', 'updated_at'])
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

        if (!after) {

            throw new Error('memory not found after touch');
        }

        expect(after.access_count).toBe(before.access_count + 1);
        expect(after.last_accessed_at.getTime()).toBeGreaterThan(before.last_accessed_at.getTime());
        expect(after.updated_at.getTime()).toBe(before.updated_at.getTime());

    });

});

describe('sql.memory: relate', () => {

    it('sp_Memory_Relate is idempotent — calling twice yields exactly one row', async () => {

        const a = await createMemory('memory a');
        const b = await createMemory('memory b');

        await ctx.proc('sp_Memory_Relate', {
            memory_id: a,
            related_memory_id: b,
            relation_verb: 'related-to',
            reason: 'first call',
        });

        await ctx.proc('sp_Memory_Relate', {
            memory_id: a,
            related_memory_id: b,
            relation_verb: 'related-to',
            reason: 'second call (no-op)',
        });

        const rows = await ctx.kysely
            .selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .execute();

        expect(rows.length).toBe(1);
        // First call's reason wins because the second call is a silent no-op.
        expect(rows[0]?.reason).toBe('first call');

    });

});

describe('sql.memory: bulk touch (TVP)', () => {

    it('sp_Memory_Bulk_Touch increments access_count and last_accessed_at for every TVP row', async () => {

        const a = await createMemory('bulk a');
        const b = await createMemory('bulk b');
        const c = await createMemory('bulk c');
        const ids = [a, b, c];

        const before = await ctx.kysely
            .selectFrom('Memory')
            .select(['memory_id', 'access_count', 'created_at', 'last_accessed_at'])
            .where('memory_id', 'in', ids)
            .execute();

        for (const row of before) {

            expect(row.access_count).toBe(0);
        }

        // SYSUTCDATETIME has sub-microsecond resolution; pause briefly so
        // last_accessed_at lands strictly after created_at.
        await new Promise((resolve) => setTimeout(resolve, 25));

        await ctx.proc('sp_Memory_Bulk_Touch', {
            MemoryIds: tvp('MemoryIdSet', ids.map((id) => ({ memory_id: id }))),
        });

        const after = await ctx.kysely
            .selectFrom('Memory')
            .select(['memory_id', 'access_count', 'created_at', 'last_accessed_at'])
            .where('memory_id', 'in', ids)
            .execute();

        expect(after.length).toBe(3);

        for (const row of after) {

            expect(row.access_count).toBe(1);
            expect(row.last_accessed_at.getTime()).toBeGreaterThan(row.created_at.getTime());
        }

    });

    it('sp_Memory_Bulk_Touch with empty TVP no-ops without error', async () => {

        const control = await createMemory('untouched control');

        await ctx.proc('sp_Memory_Bulk_Touch', {
            MemoryIds: tvp('MemoryIdSet', []),
        });

        const row = await ctx.kysely
            .selectFrom('Memory')
            .select(['access_count'])
            .where('memory_id', '=', control)
            .executeTakeFirst();

        expect(row?.access_count).toBe(0);

    });

});

describe('sql.memory: relate self-reference rejection', () => {

    it('sp_Memory_Relate rejects relating a memory to itself', async () => {

        const memoryId = await createMemory('self-rel');

        await expect(
            ctx.proc('sp_Memory_Relate', {
                memory_id: memoryId,
                related_memory_id: memoryId,
                relation_verb: 'related-to',
                reason: 'self',
            }),
        ).rejects.toThrow(/Cannot relate a memory to itself/i);

    });

});

describe('sql.memory: unrelate', () => {

    it('sp_Memory_Unrelate removes the row from Related_Memory after sp_Memory_Relate', async () => {

        const a = await createMemory('unrelate a');
        const b = await createMemory('unrelate b');

        await ctx.proc('sp_Memory_Relate', {
            memory_id: a,
            related_memory_id: b,
            relation_verb: 'related-to',
            reason: 'transient',
        });

        const before = await ctx.kysely
            .selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .execute();

        expect(before.length).toBe(1);

        await ctx.proc('sp_Memory_Unrelate', {
            memory_id: a,
            related_memory_id: b,
        });

        const after = await ctx.kysely
            .selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', a)
            .where('related_memory_id', '=', b)
            .execute();

        expect(after.length).toBe(0);

    });

});

describe('sql.memory: detach project', () => {

    it('sp_Memory_Detach_Project removes the row from Project_Memory after attach', async () => {

        const memoryId = await createMemory('detach subject');

        const projectRows = await ctx.proc('sp_Project_Create', {
            name: `detach-project-${Date.now()}`,
        });

        const projectId = projectRows[0]?.project_id;

        if (typeof projectId !== 'number') {

            throw new Error('sp_Project_Create returned no rows');
        }

        await ctx.proc('sp_Memory_Attach_Project', {
            memory_id: memoryId,
            project_id: projectId,
        });

        const attached = await ctx.kysely
            .selectFrom('Project_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .where('project_id', '=', projectId)
            .execute();

        expect(attached.length).toBe(1);

        await ctx.proc('sp_Memory_Detach_Project', {
            memory_id: memoryId,
            project_id: projectId,
        });

        const detached = await ctx.kysely
            .selectFrom('Project_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .where('project_id', '=', projectId)
            .execute();

        expect(detached.length).toBe(0);

    });

});

describe('sql.memory: consolidate', () => {

    it('sp_Memory_Consolidate rejects consolidating a memory with itself', async () => {

        const agentId = await createAgent('consolidate-self');
        const memoryId = await createMemory('self-consolidate');

        await expect(
            ctx.proc('sp_Memory_Consolidate', {
                canonical_memory_id: memoryId,
                duplicate_memory_id: memoryId,
                agent_id: agentId,
                reason: 'self',
            }),
        ).rejects.toThrow(/Cannot consolidate a memory with itself/i);

    });

    it('sp_Memory_Consolidate re-points tags + projects, marks duplicate superseded, records supersedes relation', async () => {

        const agentId = await createAgent('consolidate');

        const canonicalId = await createMemory('canonical');
        const duplicateId = await createMemory('duplicate');

        const projectRows = await ctx.proc('sp_Project_Create', {
            name: `consolidate-project-${Date.now()}`,
        });

        const projectId = projectRows[0]?.project_id;

        if (typeof projectId !== 'number') {

            throw new Error('sp_Project_Create returned no rows');
        }

        const tagRows = await ctx.proc('sp_Tag_Create', {
            name: `consolidate-tag-${Date.now()}`,
        });

        const tagId = tagRows[0]?.tag_id;

        if (typeof tagId !== 'number') {

            throw new Error('sp_Tag_Create returned no rows');
        }

        // Attach to the duplicate, NOT the canonical, so we can verify
        // the consolidate actually moves them.
        await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: duplicateId });
        await ctx.proc('sp_Memory_Attach_Project', { memory_id: duplicateId, project_id: projectId });

        await ctx.proc('sp_Memory_Consolidate', {
            canonical_memory_id: canonicalId,
            duplicate_memory_id: duplicateId,
            agent_id: agentId,
            reason: 'merging duplicate into canonical',
        });

        const tagOnDuplicate = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('memory_id', '=', duplicateId)
            .execute();

        const tagOnCanonical = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('memory_id', '=', canonicalId)
            .where('tag_id', '=', tagId)
            .execute();

        expect(tagOnDuplicate.length).toBe(0);
        expect(tagOnCanonical.length).toBe(1);

        const projectOnDuplicate = await ctx.kysely
            .selectFrom('Project_Memory')
            .selectAll()
            .where('memory_id', '=', duplicateId)
            .execute();

        const projectOnCanonical = await ctx.kysely
            .selectFrom('Project_Memory')
            .selectAll()
            .where('memory_id', '=', canonicalId)
            .where('project_id', '=', projectId)
            .execute();

        expect(projectOnDuplicate.length).toBe(0);
        expect(projectOnCanonical.length).toBe(1);

        const duplicate = await ctx.kysely
            .selectFrom('Memory')
            .select(['relevance_status'])
            .where('memory_id', '=', duplicateId)
            .executeTakeFirst();

        expect(duplicate?.relevance_status).toBe('superseded');

        const supersedes = await ctx.kysely
            .selectFrom('Related_Memory')
            .selectAll()
            .where('memory_id', '=', canonicalId)
            .where('related_memory_id', '=', duplicateId)
            .where('relation_verb', '=', 'supersedes')
            .execute();

        expect(supersedes.length).toBe(1);

    });

});
