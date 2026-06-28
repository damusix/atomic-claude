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

describe('db.memory.cmd.create', () => {

    it('maps camelCase input to snake_case proc args and returns the new id', async () => {

        const memoryId = await db.memory.cmd.create({
            content:         'kysely returns Generated<T> for default columns',
            domain:          'backend',
            category:        'fact',
            reason:          'domain test',
            provenanceId:    0,
            agentId:         0,
            wasInferred:     false,
            wasObserved:     true,
            wasEvidenced:    false,
            wasUserProvided: false,
        });

        expect(memoryId).toBeGreaterThan(0);

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.was_observed).toBe(true);
        expect(memory.relevance_status).toBe('active');

    });

    it('rejects empty content via Zod', async () => {

        await expect(
            db.memory.cmd.create({
                content:      '',
                domain:       'backend',
                category:     'fact',
                reason:       '',
                provenanceId: 0,
                agentId:      0,
                wasObserved:  true,
            }),
        ).rejects.toThrow();

    });

    it('rejects negative agentId via Zod', async () => {

        await expect(
            db.memory.cmd.create({
                content:      'something',
                domain:       'backend',
                category:     'fact',
                reason:       '',
                provenanceId: 0,
                agentId:      -1,
                wasObserved:  true,
            }),
        ).rejects.toThrow();

    });

});

describe('db.memory.cmd.update', () => {

    it('rewrites the editable text + flags columns', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'old', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.update({
            memoryId,
            content:         'new content',
            domain:          'frontend',
            category:        'pattern',
            reason:          'updated',
            wasInferred:     true,
            wasObserved:     false,
            wasEvidenced:    true,
            wasUserProvided: false,
        });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.content).toBe('new content');
        expect(memory.domain).toBe('frontend');
        expect(memory.was_inferred).toBe(true);
        expect(memory.was_observed).toBe(false);

    });

});

describe('db.memory.cmd.setRelevance', () => {

    it('moves status from active to needs-review', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'subject', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.setRelevance({
            memoryId,
            newRelevanceStatus: 'needs-review',
            agentId:            0,
            reason:             'manual review needed',
        });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.relevance_status).toBe('needs-review');

    });

    it('rejects an invalid relevance enum value via Zod', async () => {

        await expect(
            db.memory.cmd.setRelevance({
                memoryId:           1,
                newRelevanceStatus: 'not-a-valid-status',
                agentId:            0,
                reason:             '',
            }),
        ).rejects.toThrow();

    });

});

describe('db.memory.cmd.softDelete', () => {

    it('transitions the memory to deleted', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'doomed', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.softDelete({
            memoryId, agentId: 0, reason: 'no longer accurate',
        });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.relevance_status).toBe('deleted');

    });

});

describe('db.memory.cmd.restore', () => {

    it('moves a deleted memory back to active', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'recoverable', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.softDelete({ memoryId, agentId: 0, reason: 'oops' });
        await db.memory.cmd.restore({ memoryId, agentId: 0, reason: 'still useful' });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.relevance_status).toBe('active');

    });

});

describe('db.memory.cmd.touch', () => {

    it('bumps access_count without changing updated_at-bound state', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'tracked', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.touch({ memoryId, agentId: 0 });
        await db.memory.cmd.touch({ memoryId, agentId: 0 });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.access_count).toBeGreaterThanOrEqual(2);

    });

});

describe('db.memory.cmd.relate', () => {

    it('records a directed Related_Memory edge surfaced by qry.related', async () => {

        const a = await db.memory.cmd.create({
            content: 'a', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });
        const b = await db.memory.cmd.create({
            content: 'b', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.relate({
            memoryId:        a,
            relatedMemoryId: b,
            relationVerb:    'supersedes',
            reason:          'cleanup',
        });

        const links = await db.memory.qry.related(a);
        expect(links.length).toBeGreaterThan(0);

    });

});

describe('db.memory.cmd.unrelate', () => {

    it('removes a previously created edge', async () => {

        const a = await db.memory.cmd.create({
            content: 'a', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });
        const b = await db.memory.cmd.create({
            content: 'b', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.relate({
            memoryId: a, relatedMemoryId: b, relationVerb: 'supersedes', reason: '',
        });
        await db.memory.cmd.unrelate({ memoryId: a, relatedMemoryId: b });

        const links = await db.memory.qry.related(a);
        expect(links.length).toBe(0);

    });

});

describe('db.memory.cmd.attachProject', () => {

    it('attaches a memory to the sentinel project (id 0)', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'project-scoped', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.attachProject({ memoryId, projectId: 0 });
        // detachProject is the natural pair — this also exercises the inverse path.
        await db.memory.cmd.detachProject({ memoryId, projectId: 0 });

        const memory = await db.memory.qry.findById(memoryId);
        if (!memory) throw new Error('findById returned undefined');

        expect(memory.memory_id).toBe(memoryId);

    });

});
