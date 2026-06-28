import { beforeAll, beforeEach, describe, it, expect } from 'bun:test';

import { bootstrap, truncateAll } from '../helpers/test-context';

let ctx: Awaited<ReturnType<typeof bootstrap>>['ctx'];
let db:  Awaited<ReturnType<typeof bootstrap>>['db'];

beforeAll(async () => {

    ({ ctx, db } = await bootstrap());

});

beforeEach(async () => {

    await truncateAll(ctx);

});

async function seedTag(name: string): Promise<number> {

    return db.tag.cmd.create({
        name, description: '', reason: 'seed',
        provenanceId: 0, agentId: 0,
    });

}

async function seedMemory(): Promise<number> {

    return db.memory.cmd.create({
        content: 'tagged memory', domain: 'backend', category: 'fact',
        reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
    });

}

describe('db.tag.cmd.create', () => {

    it('creates a tag and returns its id', async () => {

        const tagId = await seedTag('backend-perf');

        expect(tagId).toBeGreaterThan(0);

        const tag = await db.tag.qry.findById(tagId);
        if (!tag) throw new Error('findById returned undefined');

        expect(tag.name).toBe('backend-perf');

    });

});

describe('db.tag.cmd.update', () => {

    it('rewrites name and description', async () => {

        const tagId = await seedTag('original-name');

        await db.tag.cmd.update({
            tagId, name: 'renamed', description: 'updated', reason: 'cleanup',
        });

        const tag = await db.tag.qry.findById(tagId);
        if (!tag) throw new Error('findById returned undefined');

        expect(tag.name).toBe('renamed');
        expect(tag.description).toBe('updated');

    });

});

describe('db.tag.cmd.remove', () => {

    it('hard-deletes a tag so qry.findById returns undefined', async () => {

        const tagId = await seedTag('to-be-removed');

        await db.tag.cmd.remove({ tagId });

        const tag = await db.tag.qry.findById(tagId);
        expect(tag).toBeUndefined();

    });

});

describe('db.tag.cmd attach + detach', () => {

    it('attaches a tag to a memory and detaches it again', async () => {

        const tagId    = await seedTag('attach-target');
        const memoryId = await seedMemory();

        await db.tag.cmd.attachMemory({ tagId, memoryId });

        const after = await db.tag.qry.listForMemory(memoryId);
        expect(after.some((row) => row.tag_id === tagId)).toBe(true);

        await db.tag.cmd.detachMemory({ tagId, memoryId });

        const cleared = await db.tag.qry.listForMemory(memoryId);
        expect(cleared.some((row) => row.tag_id === tagId)).toBe(false);

    });

});

describe('db.tag.cmd.merge', () => {

    it('re-points memory attachments from source to target then deletes source', async () => {

        const sourceId = await seedTag('source-tag');
        const targetId = await seedTag('target-tag');
        const memoryId = await seedMemory();

        await db.tag.cmd.attachMemory({ tagId: sourceId, memoryId });

        await db.tag.cmd.merge({
            sourceTagId: sourceId,
            targetTagId: targetId,
            agentId:     0,
            reason:      'consolidate duplicates',
        });

        // Source is gone.
        const source = await db.tag.qry.findById(sourceId);
        expect(source).toBeUndefined();

        // Target now carries the memory attachment.
        const memoryTags = await db.tag.qry.listForMemory(memoryId);
        expect(memoryTags.some((row) => row.tag_id === targetId)).toBe(true);

    });

});

describe('Zod / SQL failure modes on tag inputs', () => {

    it('rejects a duplicate name at the SQL layer (UNIQUE constraint)', async () => {

        await seedTag('unique-name');

        await expect(seedTag('unique-name')).rejects.toThrow();

    });

    it('rejects a name longer than 255 chars via Zod', async () => {

        await expect(
            db.tag.cmd.create({
                name:         'x'.repeat(256),
                description:  '',
                reason:       '',
                provenanceId: 0,
                agentId:      0,
            }),
        ).rejects.toThrow();

    });

});
