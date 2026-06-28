/**
 * Table-valued function tests via ctx.tvf(...).
 *
 * Covers tvf_FilterMemoriesByTags — relational division: returns memories
 * that carry every requested tag. Verifies the EXISTS guard for empty
 * input and the rank column from fn_MemoryRank.
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
        name, description: '', reason: 'fixture',
        provenance_id: 0, agent_id: 0,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('tag create failed');
    }

    return created.tag_id;

}

async function makeMemory(content: string): Promise<number> {

    const rows = await ctx.proc('sp_Memory_Create', {
        content, domain: 'backend', category: 'fact',
        reason: 'fixture', provenance_id: 0, agent_id: 0,
        was_inferred: false, was_observed: true,
        was_evidenced: false, was_user_provided: false,
    });

    const created = rows[0];

    if (!created) {

        throw new Error('memory create failed');
    }

    return created.memory_id;

}

async function attach(tagId: number, memoryId: number): Promise<void> {

    await ctx.proc('sp_Tag_Attach_Memory', { tag_id: tagId, memory_id: memoryId });

}

describe('sql: tvf_FilterMemoriesByTags', () => {

    it('returns only memories that have ALL requested tags (relational division)', async () => {

        const stamp = Date.now();
        const tagA = await makeTag(`tvf-A-${stamp}`);
        const tagB = await makeTag(`tvf-B-${stamp}`);

        const m1 = await makeMemory('m1-AB');     // tagged A + B
        const m2 = await makeMemory('m2-A-only'); // tagged A only

        await attach(tagA, m1);
        await attach(tagB, m1);
        await attach(tagA, m2);

        const rows = await ctx.tvf('tvf_FilterMemoriesByTags', {
            TagIds: tvp('TagIdSet', [
                { tag_id: tagA },
                { tag_id: tagB },
            ]),
        });

        expect(rows.length).toBe(1);

        const [row] = rows;

        if (!row) {

            throw new Error('expected one row');
        }

        expect(row.memory_id).toBe(m1);
        expect(row.content).toBe('m1-AB');

    });

    it('returns zero rows for an empty TVP (EXISTS guard prevents vacuous match)', async () => {

        // Seed some memories so the result *could* be all rows if the guard
        // were missing. The guard makes it deliberately empty.
        const tagA = await makeTag(`tvf-empty-${Date.now()}`);
        const m1 = await makeMemory('present');

        await attach(tagA, m1);

        const rows = await ctx.tvf('tvf_FilterMemoriesByTags', {
            TagIds: tvp('TagIdSet', []),
        });

        expect(rows.length).toBe(0);

    });

    it('returns zero rows when no memory carries the requested tag', async () => {

        const tagA = await makeTag(`tvf-miss-A-${Date.now()}`);
        const tagB = await makeTag(`tvf-miss-B-${Date.now()}`);

        const m1 = await makeMemory('only-A');

        await attach(tagA, m1);

        // Asking for tagB alone — m1 doesn't have it.
        const rows = await ctx.tvf('tvf_FilterMemoriesByTags', {
            TagIds: tvp('TagIdSet', [{ tag_id: tagB }]),
        });

        expect(rows.length).toBe(0);

    });

    it('returns a numeric rank > 0 for an active confident memory', async () => {

        const tagId = await makeTag(`tvf-rank-${Date.now()}`);

        // Pump the confidence score to push fn_MemoryRank above zero.
        const memRows = await ctx.proc('sp_Memory_Create', {
            content: 'ranked', domain: 'backend', category: 'fact',
            reason: '', provenance_id: 0, agent_id: 0,
            was_inferred: true, was_observed: true,
            was_evidenced: true, was_user_provided: true,
        });

        const created = memRows[0];

        if (!created) {

            throw new Error('memory create failed');
        }

        await attach(tagId, created.memory_id);

        const rows = await ctx.tvf('tvf_FilterMemoriesByTags', {
            TagIds: tvp('TagIdSet', [{ tag_id: tagId }]),
        });

        expect(rows.length).toBe(1);

        const [row] = rows;

        if (!row) {

            throw new Error('expected one row');
        }

        expect(typeof row.rank).toBe('number');
        expect(row.rank).toBeGreaterThan(0);
        expect(row.rank).toBeLessThanOrEqual(1);

    });

});
