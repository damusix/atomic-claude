/**
 * Layer 2 — MemoryCommands / MemoryQueries facade.
 *
 * Asserts the Zod boundary, camelCase mapping, and that round-trips through
 * the public facade observably mutate state. SQL-contract correctness is
 * covered by Layer 1; this layer verifies that callers using
 * `db.memory.cmd/qry` see the right shape and the right side effects.
 */
import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';

import { bootstrap, resetApplicationData } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let db: Awaited<ReturnType<typeof bootstrap>>['db'];

beforeAll(async () => {

    ({ ctx, db } = await bootstrap());

});



beforeEach(async () => {

    await resetApplicationData(ctx);

});

describe('memory: facade lifecycle', () => {

    it('db.memory.cmd.create + qry.findById round-trip', async () => {

        const created = await db.memory.cmd.create({
            content: 'User prefers dark mode',
            domain: 'frontend',
            category: 'preference',
            reason: 'happy path',
        });

        expect(created.memory_id).toBeGreaterThan(0);

        const fetched = await db.memory.qry.findById(created.memory_id);

        expect(fetched).toBeDefined();
        expect(fetched?.content).toBe('User prefers dark mode');
        expect(fetched?.domain).toBe('frontend');
        expect(fetched?.category).toBe('preference');
        expect(fetched?.relevance_status).toBe('active');

    });

    it('db.memory.qry.listActive returns at least one row after seeding', async () => {

        await db.memory.cmd.create({
            content: 'fact one',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.create({
            content: 'fact two',
            domain: 'backend',
            category: 'fact',
        });

        const rows = await db.memory.qry.listActive();

        expect(rows.length).toBeGreaterThanOrEqual(2);
        const contents = rows.map((r) => r.content);
        expect(contents).toContain('fact one');
        expect(contents).toContain('fact two');

    });

    it('db.memory.qry.findByIdWithConfidence returns vw_Memory shape', async () => {

        const { memory_id } = await db.memory.cmd.create({
            content: 'evidence-backed claim',
            domain: 'backend',
            category: 'fact',
            wasEvidenced: true,
            wasObserved: true,
        });

        const enriched = await db.memory.qry.findByIdWithConfidence(memory_id);

        expect(enriched).toBeDefined();
        expect(enriched?.memory_id).toBe(memory_id);
        expect(enriched?.confidence).toBeGreaterThanOrEqual(2);

    });

    it('db.memory.qry.confidence + rank scalars resolve', async () => {

        const { memory_id } = await db.memory.cmd.create({
            content: 'three-flag memory',
            domain: 'backend',
            category: 'fact',
            wasInferred: true,
            wasEvidenced: true,
            wasUserProvided: true,
        });

        const conf = await db.memory.qry.confidence(memory_id);
        const rank = await db.memory.qry.rank(memory_id);

        expect(conf.confidence).toBe(3);
        expect(typeof rank.rank).toBe('number');

    });

});

describe('memory: update happy-path', () => {

    it('db.memory.cmd.update mutates content + bumps updated_at via sp_Memory_Update', async () => {

        const created = await db.memory.cmd.create({
            content: 'original',
            domain: 'backend',
            category: 'fact',
            reason: 'seed',
        });

        const before = await db.memory.qry.findById(created.memory_id);
        expect(before?.content).toBe('original');

        // Sleep a moment so updated_at is observably greater than created_at.
        await new Promise((resolve) => setTimeout(resolve, 25));

        await db.memory.cmd.update({
            memoryId: created.memory_id,
            content: 'updated',
            domain: 'backend',
            category: 'fact',
            reason: 'edit',
        });

        const after = await db.memory.qry.findById(created.memory_id);
        expect(after?.content).toBe('updated');
        expect(after?.domain).toBe('backend');
        expect(after?.category).toBe('fact');
        expect(after?.updated_at).toBeDefined();
        expect(after?.created_at).toBeDefined();
        if (after?.updated_at && after?.created_at) {

            expect(after.updated_at.getTime()).toBeGreaterThan(after.created_at.getTime());
        }

    });

});

describe('memory: state machine', () => {

    it('db.memory.cmd.setRelevance moves active -> needs-review and qry sees the new status', async () => {

        const agent = await db.agent.cmd.create({ name: `mem-agent-${Date.now()}` });

        const { memory_id } = await db.memory.cmd.create({
            content: 'transition target',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.setRelevance({
            memoryId: memory_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
            reason: 'review queue',
        });

        const after = await db.memory.qry.findById(memory_id);
        expect(after?.relevance_status).toBe('needs-review');

    });

    it('db.memory.cmd.delete soft-deletes via SetRelevance and restore brings it back', async () => {

        const agent = await db.agent.cmd.create({ name: `mem-life-${Date.now()}` });

        const { memory_id } = await db.memory.cmd.create({
            content: 'delete then restore',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.delete({ memoryId: memory_id, agentId: agent.agent_id });
        const deleted = await db.memory.qry.findById(memory_id);
        expect(deleted?.relevance_status).toBe('deleted');

        await db.memory.cmd.restore({ memoryId: memory_id, agentId: agent.agent_id });
        const restored = await db.memory.qry.findById(memory_id);
        expect(restored?.relevance_status).toBe('active');

    });

    it('db.memory.cmd.touch bumps access_count', async () => {

        const { memory_id } = await db.memory.cmd.create({
            content: 'touch me',
            domain: 'backend',
            category: 'fact',
        });

        const before = await db.memory.qry.findById(memory_id);
        expect(before?.access_count).toBe(0);

        await db.memory.cmd.touch({ memoryId: memory_id });
        await db.memory.cmd.touch({ memoryId: memory_id });

        const after = await db.memory.qry.findById(memory_id);
        expect(after?.access_count).toBe(2);

    });

});

describe('memory: relations', () => {

    it('db.memory.cmd.relate inserts symmetric Related_Memory and listRelated returns it', async () => {

        const a = await db.memory.cmd.create({
            content: 'left',
            domain: 'backend',
            category: 'fact',
        });
        const b = await db.memory.cmd.create({
            content: 'right',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.relate({
            memoryId: a.memory_id,
            relatedMemoryId: b.memory_id,
            relationVerb: 'related-to',
            reason: 'unit test',
        });

        const related = await db.memory.qry.listRelated(a.memory_id);
        expect(related.length).toBeGreaterThanOrEqual(1);
        expect(related[0]?.related_memory_id).toBe(b.memory_id);

    });

    it('db.memory.cmd.unrelate is idempotent (no throw on missing pair)', async () => {

        const a = await db.memory.cmd.create({
            content: 'a',
            domain: 'backend',
            category: 'fact',
        });
        const b = await db.memory.cmd.create({
            content: 'b',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.unrelate({
            memoryId: a.memory_id,
            relatedMemoryId: b.memory_id,
        });

    });

    it('db.memory.cmd.bulkTouch increments access_count for every supplied id', async () => {

        const ids: number[] = [];
        for (let i = 0; i < 3; i += 1) {

            const { memory_id } = await db.memory.cmd.create({
                content: `bulk-${i}`,
                domain: 'backend',
                category: 'fact',
            });
            ids.push(memory_id);
        }

        await db.memory.cmd.bulkTouch({ memoryIds: ids });

        for (const id of ids) {

            const row = await db.memory.qry.findById(id);
            expect(row?.access_count).toBe(1);
        }

    });

});

describe('memory: zod boundary', () => {

    it('db.memory.cmd.create rejects empty content (.min(1))', async () => {

        await expect(db.memory.cmd.create({
            content: '',
            domain: 'backend',
            category: 'fact',
        })).rejects.toThrow();

    });

    it('db.memory.cmd.update rejects negative memoryId (.positive())', async () => {

        await expect(db.memory.cmd.update({
            memoryId: -1,
            content: 'noop',
            domain: 'backend',
            category: 'fact',
        })).rejects.toThrow();

    });

    it('db.memory.cmd.bulkTouch rejects empty memoryIds (.min(1))', async () => {

        await expect(db.memory.cmd.bulkTouch({ memoryIds: [] })).rejects.toThrow();

    });

    it('db.memory.cmd.relate rejects empty relationVerb', async () => {

        await expect(db.memory.cmd.relate({
            memoryId: 1,
            relatedMemoryId: 2,
            relationVerb: '',
        })).rejects.toThrow();

    });

});
