/**
 * TVP edge-case tests for sp_Tag_Bulk_Attach_Memory.
 *
 * Exercises the SDK's TVP pre-flight: row-key consistency, MSSQL's
 * 2,100 parameter cap, and the proc's idempotent INSERT...WHERE NOT EXISTS
 * shape. Verifies we read back the expected Memory_Tag state — never
 * "didn't throw" alone.
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

async function makeTag(name: string): Promise<number> {

    const rows = await ctx.proc('sp_Tag_Create', {
        name,
        description: '',
        reason: 'fixture',
        provenance_id: 0,
        agent_id: 0,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('sp_Tag_Create returned no rows');
    }

    return created.tag_id;

}

async function makeMemory(content: string): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content,
        domain: 'backend',
        category: 'fact',
        reason: 'fixture',
        provenance_id: 0,
        agent_id: 0,
        was_inferred: false,
        was_observed: false,
        was_evidenced: false,
        was_user_provided: false,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('sp_Memory_Create returned no rows');
    }

    return created.memory_id;

}

describe('sql: tag TVP edge cases', () => {

    it('empty TVP no-ops without error and inserts no rows', async () => {

        const tagId = await makeTag(`empty-tvp-${Date.now()}`);

        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp('TagAttachmentInput', []),
        });

        const rows = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(0);

    });

    it('single-row TVP inserts exactly one Memory_Tag row', async () => {

        const tagId = await makeTag(`single-${Date.now()}`);
        const memoryId = await makeMemory('single-row-tvp');

        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp('TagAttachmentInput', [
                { tag_id: tagId, entity_id: memoryId },
            ]),
        });

        const rows = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(1);

        const [row] = rows;

        if (!row) {

            throw new Error('expected one Memory_Tag row');
        }

        expect(row.memory_id).toBe(memoryId);

    });

    it('200 rows (400 params) inserts every row under the 2100 limit', async () => {

        const tagId = await makeTag(`bulk-200-${Date.now()}`);

        // Bulk-insert 200 Memory rows via Kysely so we get IDs in two round-trips
        // (insert + select) instead of paying the per-proc round-trip cost.
        // 200 rows × 2 cols = 400 params, comfortably under the 2,100 cap and
        // small enough that the next beforeEach() truncate cleans up quickly.
        await ctx.kysely.insertInto('Memory')
            .values(Array.from({ length: 200 }, (_, i) => ({
                domain: 'backend',
                category: 'fact',
                relevance_status: 'active',
                content: `bulk-${i}`,
                reason: '',
            })))
            .execute();

        const inserted = await ctx.kysely.selectFrom('Memory')
            .select('memory_id')
            .where('content', 'like', 'bulk-%')
            .orderBy('memory_id', 'asc')
            .execute();

        const memoryIds = inserted.map((r) => r.memory_id);

        expect(memoryIds.length).toBe(200);

        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp(
                'TagAttachmentInput',
                memoryIds.map((id) => ({ tag_id: tagId, entity_id: id })),
            ),
        });

        const countRow = await ctx.kysely
            .selectFrom('Memory_Tag')
            .select(({ fn }) => fn.countAll<number>().as('c'))
            .where('tag_id', '=', tagId)
            .executeTakeFirstOrThrow();

        expect(Number(countRow.c)).toBe(200);

    }, 30_000);

    it('1100 rows (2200 params) triggers the SDK 2100-limit pre-flight error', async () => {

        const tagId = await makeTag(`oversize-${Date.now()}`);

        const oversize = Array.from({ length: 1100 }, (_, i) => ({
            tag_id: tagId,
            entity_id: i + 1,
        }));

        await expect(
            ctx.proc('sp_Tag_Bulk_Attach_Memory', {
                Pairs: tvp('TagAttachmentInput', oversize),
            }),
        ).rejects.toThrow(/2100|parameter/i);

        // Pre-flight rejects before sending — Memory_Tag must still be empty.
        const rows = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(0);

    });

    it('idempotent on second call — WHERE NOT EXISTS skips duplicate pairs', async () => {

        const tagId = await makeTag(`idem-${Date.now()}`);
        const m1 = await makeMemory('idem-1');
        const m2 = await makeMemory('idem-2');
        const m3 = await makeMemory('idem-3');

        const pairs = [m1, m2, m3].map((id) => ({ tag_id: tagId, entity_id: id }));

        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp('TagAttachmentInput', pairs),
        });

        // Second call: same pairs.
        await ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp('TagAttachmentInput', pairs),
        });

        const rows = await ctx.kysely
            .selectFrom('Memory_Tag')
            .selectAll()
            .where('tag_id', '=', tagId)
            .execute();

        expect(rows.length).toBe(3);

        const memoryIds = rows.map((r) => r.memory_id).sort((a, b) => a - b);

        expect(memoryIds).toEqual([m1, m2, m3].sort((a, b) => a - b));

    });

    it('throws client-side when row keys are inconsistent', async () => {

        // Mismatched keys — the second row is missing entity_id. tvp() should
        // throw synchronously before the proc call ever happens.
        expect(() => tvp('TagAttachmentInput', [
            { tag_id: 1, entity_id: 2 },
            { tag_id: 1 },
        ])).toThrow(/mismatched keys|expected/i);

    });

});
