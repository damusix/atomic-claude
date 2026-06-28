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
        title: 'Task milestone', provenanceId: 0, agentId: 0,
    });

}

describe('db.task.cmd.create', () => {

    it('returns the composite { milestoneId, taskNo } primary key', async () => {

        const milestoneId = await seedMilestone();

        const created = await db.task.cmd.create({
            milestoneId, title: 'Wire DT importer', agentId: 0,
        });

        expect(created.milestoneId).toBe(milestoneId);
        expect(created.taskNo).toBeGreaterThan(0);

        const row = await db.task.qry.findById(created.milestoneId, created.taskNo);
        if (!row) throw new Error('findById returned undefined');

        expect(row.title).toBe('Wire DT importer');
        expect(row.tracking_status).toBe('not-started');

    });

});

describe('db.task.cmd.update + setTracking + softDelete', () => {

    it('rewrites text columns via update', async () => {

        const milestoneId = await seedMilestone();
        const { taskNo } = await db.task.cmd.create({
            milestoneId, title: 'subject', agentId: 0,
        });

        await db.task.cmd.update({
            milestoneId, taskNo,
            title: 'revised', content: 'body', reason: 'edit',
        });

        const row = await db.task.qry.findById(milestoneId, taskNo);
        if (!row) throw new Error('findById returned undefined');

        expect(row.title).toBe('revised');
        expect(row.content).toBe('body');

    });

    it('moves tracking_status to in-progress via setTracking', async () => {

        const milestoneId = await seedMilestone();
        const { taskNo } = await db.task.cmd.create({
            milestoneId, title: 'subject', agentId: 0,
        });

        await db.task.cmd.setTracking({
            milestoneId, taskNo, newTrackingStatus: 'in-progress',
            agentId: 0, reason: 'started',
        });

        const row = await db.task.qry.findById(milestoneId, taskNo);
        if (!row) throw new Error('findById returned undefined');

        expect(row.tracking_status).toBe('in-progress');

    });

    it('marks the task abandoned via softDelete', async () => {

        const milestoneId = await seedMilestone();
        const { taskNo } = await db.task.cmd.create({
            milestoneId, title: 'doomed', agentId: 0,
        });

        await db.task.cmd.softDelete({
            milestoneId, taskNo, agentId: 0, reason: 'duplicate',
        });

        const row = await db.task.qry.findById(milestoneId, taskNo);
        if (!row) throw new Error('findById returned undefined');

        expect(row.tracking_status).toBe('abandoned');

    });

});

describe('db.task.cmd.depend + undepend', () => {

    it('records a dependency edge surfaced via listDependencies', async () => {

        const milestoneId = await seedMilestone();
        const a = await db.task.cmd.create({
            milestoneId, title: 'A', agentId: 0,
        });
        const b = await db.task.cmd.create({
            milestoneId, title: 'B', agentId: 0,
        });

        await db.task.cmd.depend({
            milestoneId:    a.milestoneId,
            taskNo:         a.taskNo,
            depMilestoneId: b.milestoneId,
            depTaskNo:      b.taskNo,
            dependencyVerb: 'blocks',
            reason:         'sequence',
        });

        const deps = await db.task.qry.listDependencies(a.milestoneId, a.taskNo);
        expect(deps.length).toBe(1);

        const dependents = await db.task.qry.listDependents(b.milestoneId, b.taskNo);
        expect(dependents.length).toBe(1);

        await db.task.cmd.undepend({
            milestoneId:    a.milestoneId,
            taskNo:         a.taskNo,
            depMilestoneId: b.milestoneId,
            depTaskNo:      b.taskNo,
        });

        const cleared = await db.task.qry.listDependencies(a.milestoneId, a.taskNo);
        expect(cleared.length).toBe(0);

    });

});

describe('db.task.qry queries', () => {

    it('backlog returns rows for active milestone tasks', async () => {

        const milestoneId = await seedMilestone();
        await db.task.cmd.create({
            milestoneId, title: 'backlog candidate', agentId: 0,
        });

        const backlog = await db.task.qry.backlog({ limit: 50 });
        expect(backlog.length).toBeGreaterThan(0);

    });

    it('nextTaskNo returns 1 for an empty milestone and increments after a create', async () => {

        const milestoneId = await seedMilestone();

        const before = await db.task.qry.nextTaskNo(milestoneId);
        expect(before).toBe(1);

        await db.task.cmd.create({ milestoneId, title: 't1', agentId: 0 });

        const after = await db.task.qry.nextTaskNo(milestoneId);
        expect(after).toBe(2);

    });

    it('wouldCycle returns false when no edge exists yet and true after a reverse edge', async () => {

        const milestoneId = await seedMilestone();
        const a = await db.task.cmd.create({
            milestoneId, title: 'A', agentId: 0,
        });
        const b = await db.task.cmd.create({
            milestoneId, title: 'B', agentId: 0,
        });

        const safe = await db.task.qry.wouldCycle({
            milestoneId:    a.milestoneId,
            taskNo:         a.taskNo,
            depMilestoneId: b.milestoneId,
            depTaskNo:      b.taskNo,
        });
        expect(safe).toBe(false);

        // A → B exists; now B → A would cycle.
        await db.task.cmd.depend({
            milestoneId:    a.milestoneId,
            taskNo:         a.taskNo,
            depMilestoneId: b.milestoneId,
            depTaskNo:      b.taskNo,
            dependencyVerb: 'blocks',
            reason:         '',
        });

        const cycle = await db.task.qry.wouldCycle({
            milestoneId:    b.milestoneId,
            taskNo:         b.taskNo,
            depMilestoneId: a.milestoneId,
            depTaskNo:      a.taskNo,
        });
        expect(cycle).toBe(true);

    });

});

describe('Zod failures on task inputs', () => {

    it('rejects empty title on create via Zod', async () => {

        await expect(
            db.task.cmd.create({
                milestoneId: 1, title: '', agentId: 0,
            }),
        ).rejects.toThrow();

    });

    it('rejects an unknown dependencyVerb on depend via Zod', async () => {

        await expect(
            db.task.cmd.depend({
                milestoneId: 1, taskNo: 2,
                depMilestoneId: 1, depTaskNo: 3,
                dependencyVerb: 'unknown-verb', reason: '',
            }),
        ).rejects.toThrow();

    });

});
