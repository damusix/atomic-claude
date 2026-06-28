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

describe('db.audit.qry.milestoneHistory', () => {

    it('returns the tracking transition produced by setTracking', async () => {

        const milestoneId = await db.milestone.cmd.create({
            title: 'audited milestone', provenanceId: 0, agentId: 0,
        });

        await db.milestone.cmd.setTracking({
            milestoneId, newTrackingStatus: 'in-progress',
            agentId: 0, reason: 'starting work',
        });

        const log = await db.audit.qry.milestoneHistory({ milestoneId });

        expect(log.length).toBeGreaterThan(0);
        expect(log.some((row) => row.to_status === 'in-progress')).toBe(true);

    });

});

describe('db.audit.qry.taskHistory', () => {

    it('returns the tracking transition produced by task.setTracking', async () => {

        const milestoneId = await db.milestone.cmd.create({
            title: 'host milestone', provenanceId: 0, agentId: 0,
        });
        const { taskNo } = await db.task.cmd.create({
            milestoneId, title: 'audited task', agentId: 0,
        });

        await db.task.cmd.setTracking({
            milestoneId, taskNo, newTrackingStatus: 'in-progress',
            agentId: 0, reason: 'started',
        });

        const log = await db.audit.qry.taskHistory({ milestoneId, taskNo });

        expect(log.length).toBeGreaterThan(0);
        expect(log.some((row) => row.to_status === 'in-progress')).toBe(true);

    });

});

describe('db.audit.qry.memoryHistory', () => {

    it('returns the relevance transition produced by setRelevance', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'audited memory', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.setRelevance({
            memoryId, newRelevanceStatus: 'needs-review',
            agentId: 0, reason: 'audit needed',
        });

        const log = await db.audit.qry.memoryHistory({ memoryId });

        expect(log.length).toBeGreaterThan(0);
        expect(log.some((row) => row.to_status === 'needs-review')).toBe(true);

    });

});

describe('db.audit.qry.agentActivity', () => {

    it('reports a transitions_made count after the agent has acted', async () => {

        const agentId = await db.agent.cmd.create({
            name: 'audit-agent', description: 'agent under audit',
        });

        const memoryId = await db.memory.cmd.create({
            content: 'agent memory', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId, wasObserved: true,
        });

        await db.memory.cmd.setRelevance({
            memoryId, newRelevanceStatus: 'needs-review',
            agentId, reason: 'agent action',
        });

        const stats = await db.audit.qry.agentActivity(agentId);
        if (!stats) throw new Error('agentActivity returned undefined');

        expect(stats.transitions_made).toBeGreaterThan(0);

    });

});

describe('db.audit.qry.recentActivity', () => {

    it('includes the entity_type of an entity that was just created', async () => {

        await db.memory.cmd.create({
            content: 'recent memory', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        const feed = await db.audit.qry.recentActivity({
            entityType: 'memory', limit: 50,
        });

        expect(feed.length).toBeGreaterThan(0);
        expect(feed.every((row) => row.entity_type === 'memory')).toBe(true);

    });

});

describe('db.audit.qry.recoveries', () => {

    it('surfaces a transition that moved away from the deleted state', async () => {

        const memoryId = await db.memory.cmd.create({
            content: 'recovered memory', domain: 'backend', category: 'fact',
            reason: 'seed', provenanceId: 0, agentId: 0, wasObserved: true,
        });

        await db.memory.cmd.softDelete({
            memoryId, agentId: 0, reason: 'remove',
        });
        await db.memory.cmd.restore({
            memoryId, agentId: 0, reason: 'put back',
        });

        const recoveries = await db.audit.qry.recoveries({ limit: 50 });

        expect(recoveries.length).toBeGreaterThan(0);
        expect(recoveries.every((row) => row.from_status === 'deleted')).toBe(true);

    });

});
