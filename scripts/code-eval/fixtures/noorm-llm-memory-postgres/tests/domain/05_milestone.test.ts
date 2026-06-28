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

async function seedMilestone(): Promise<number> {

    return db.milestone.cmd.create({
        title: 'Q1 launch', provenanceId: 0, agentId: 0,
    });

}

describe('db.milestone.cmd.create', () => {

    it('creates and returns the new milestone id', async () => {

        const milestoneId = await seedMilestone();

        expect(milestoneId).toBeGreaterThan(0);

        const row = await db.milestone.qry.findById(milestoneId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.title).toBe('Q1 launch');
        expect(row.tracking_status).toBe('not-started');
        expect(row.relevance_status).toBe('active');

    });

});

describe('db.milestone.cmd.update', () => {

    it('rewrites the editable text columns', async () => {

        const milestoneId = await seedMilestone();

        await db.milestone.cmd.update({
            milestoneId,
            title:   'Q1 launch (revised)',
            content: 'updated body',
            reason:  'scope cut',
        });

        const row = await db.milestone.qry.findById(milestoneId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.title).toBe('Q1 launch (revised)');
        expect(row.content).toBe('updated body');

    });

});

describe('db.milestone.cmd.setTracking', () => {

    it('moves tracking_status to in-progress', async () => {

        const milestoneId = await seedMilestone();

        await db.milestone.cmd.setTracking({
            milestoneId,
            newTrackingStatus: 'in-progress',
            agentId:           0,
            reason:            'work started',
        });

        const row = await db.milestone.qry.findById(milestoneId);
        if (!row) throw new Error('findById returned undefined');

        expect(row.tracking_status).toBe('in-progress');

    });

});

describe('db.milestone.cmd.close (cascade)', () => {

    it('closes the milestone and abandons every still-open child task', async () => {

        const milestoneId = await seedMilestone();

        // Move milestone → in-progress so close can transition to done.
        await db.milestone.cmd.setTracking({
            milestoneId, newTrackingStatus: 'in-progress',
            agentId: 0, reason: 'start',
        });

        const taskA = await db.task.cmd.create({
            milestoneId, title: 'open task A', agentId: 0,
        });
        const taskB = await db.task.cmd.create({
            milestoneId, title: 'open task B', agentId: 0,
        });

        await db.milestone.cmd.close({
            milestoneId, agentId: 0, reason: 'shipped',
        });

        const m = await db.milestone.qry.findById(milestoneId);
        if (!m) throw new Error('findById returned undefined');
        expect(m.tracking_status).toBe('done');
        expect(m.relevance_status).toBe('superseded');

        const a = await db.task.qry.findById(taskA.milestoneId, taskA.taskNo);
        if (!a) throw new Error('task A not found');
        expect(a.tracking_status).toBe('abandoned');

        const b = await db.task.qry.findById(taskB.milestoneId, taskB.taskNo);
        if (!b) throw new Error('task B not found');
        expect(b.tracking_status).toBe('abandoned');

    });

});

describe('db.milestone.cmd.attachProject + detachProject', () => {

    it('round-trips a milestone ↔ project association', async () => {

        const milestoneId = await seedMilestone();
        const projectId   = await db.project.cmd.create({
            name: 'milestone-host', agentId: 0,
        });

        await db.milestone.cmd.attachProject({ milestoneId, projectId });

        const onProject = await db.milestone.qry.listForProject(projectId, { limit: 50 });
        expect(onProject.some((row) => row.milestone_id === milestoneId)).toBe(true);

        await db.milestone.cmd.detachProject({ milestoneId, projectId });

        const cleared = await db.milestone.qry.listForProject(projectId, { limit: 50 });
        expect(cleared.some((row) => row.milestone_id === milestoneId)).toBe(false);

    });

});

describe('Zod failures on milestone inputs', () => {

    it('rejects empty title on create via Zod', async () => {

        await expect(
            db.milestone.cmd.create({
                title: '', provenanceId: 0, agentId: 0,
            }),
        ).rejects.toThrow();

    });

    it('rejects an invalid trackingStatus enum on setTracking via Zod', async () => {

        await expect(
            db.milestone.cmd.setTracking({
                milestoneId: 1, newTrackingStatus: 'not-a-status',
                agentId: 0, reason: '',
            }),
        ).rejects.toThrow();

    });

});
