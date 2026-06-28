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

async function seedArtifact(): Promise<number> {

    return db.artifact.cmd.create({
        title:        'design.md',
        description:  'system design',
        filepath:     'docs/design.md',
        reason:       'seed',
        provenanceId: 0,
        agentId:      0,
    });

}

async function seedMilestone(): Promise<number> {

    return db.milestone.cmd.create({
        title: 'Artifact milestone', provenanceId: 0, agentId: 0,
    });

}

describe('db.artifact.cmd.create', () => {

    it('creates and returns the new artifact id', async () => {

        const artifactId = await seedArtifact();

        expect(artifactId).toBeGreaterThan(0);

        const row = await db.artifact.qry.findById(artifactId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.title).toBe('design.md');
        expect(row.relevance_status).toBe('active');

    });

});

describe('db.artifact.cmd.update', () => {

    it('rewrites mutable text columns', async () => {

        const artifactId = await seedArtifact();

        await db.artifact.cmd.update({
            artifactId,
            title:       'design.md',
            description: 'expanded',
            filepath:    'docs/design.md',
            reason:      'expansion',
        });

        const row = await db.artifact.qry.findById(artifactId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.description).toBe('expanded');

    });

});

describe('db.artifact.cmd setRelevance + softDelete + restore', () => {

    it('round-trips relevance through deleted then back to active', async () => {

        const artifactId = await seedArtifact();

        await db.artifact.cmd.setRelevance({
            artifactId, newRelevanceStatus: 'needs-review',
            agentId: 0, reason: 'audit',
        });

        let row = await db.artifact.qry.findById(artifactId);
        if (!row) throw new Error('findById returned undefined');
        expect(row.relevance_status).toBe('needs-review');

        await db.artifact.cmd.softDelete({
            artifactId, agentId: 0, reason: 'obsolete',
        });

        row = await db.artifact.qry.findById(artifactId);
        if (!row) throw new Error('findById returned undefined');
        expect(row.relevance_status).toBe('deleted');

        await db.artifact.cmd.restore({
            artifactId, agentId: 0, reason: 'still useful',
        });

        row = await db.artifact.qry.findById(artifactId);
        if (!row) throw new Error('findById returned undefined');
        expect(row.relevance_status).toBe('active');

    });

});

describe('db.artifact.cmd attach + detach milestone', () => {

    it('attaches an artifact to a milestone and detaches again', async () => {

        const artifactId  = await seedArtifact();
        const milestoneId = await seedMilestone();

        await db.artifact.cmd.attachMilestone({ artifactId, milestoneId });

        const onMilestone = await db.artifact.qry.listForMilestone(milestoneId);
        expect(onMilestone.some((row) => row.artifact_id === artifactId)).toBe(true);

        await db.artifact.cmd.detachMilestone({ artifactId, milestoneId });

        const cleared = await db.artifact.qry.listForMilestone(milestoneId);
        expect(cleared.some((row) => row.artifact_id === artifactId)).toBe(false);

    });

});

describe('db.artifact.cmd attach + detach task', () => {

    it('attaches an artifact to a task and detaches again', async () => {

        const artifactId  = await seedArtifact();
        const milestoneId = await seedMilestone();
        const { taskNo }  = await db.task.cmd.create({
            milestoneId, title: 'task with artifact', agentId: 0,
        });

        await db.artifact.cmd.attachTask({ artifactId, milestoneId, taskNo });

        const onTask = await db.artifact.qry.listForTask(milestoneId, taskNo);
        expect(onTask.some((row) => row.artifact_id === artifactId)).toBe(true);

        await db.artifact.cmd.detachTask({ artifactId, milestoneId, taskNo });

        const cleared = await db.artifact.qry.listForTask(milestoneId, taskNo);
        expect(cleared.some((row) => row.artifact_id === artifactId)).toBe(false);

    });

});

describe('Zod failures on artifact inputs', () => {

    it('rejects empty title on create via Zod', async () => {

        await expect(
            db.artifact.cmd.create({
                title:        '',
                description:  '',
                filepath:     '',
                reason:       '',
                provenanceId: 0,
                agentId:      0,
            }),
        ).rejects.toThrow();

    });

    it('rejects an invalid relevance enum on setRelevance via Zod', async () => {

        await expect(
            db.artifact.cmd.setRelevance({
                artifactId: 1,
                newRelevanceStatus: 'not-a-status',
                agentId: 0,
                reason: '',
            }),
        ).rejects.toThrow();

    });

});
