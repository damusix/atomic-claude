/**
 * Layer 2 — ArtifactCommands / ArtifactQueries facade.
 *
 * Verifies the relevance state machine and idempotent attach/detach to
 * Milestone and Task. Zod boundary covers .min(1) on title and .positive()
 * on the FK ids.
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

describe('artifact: lifecycle', () => {

    it('db.artifact.cmd.create + qry.findById round-trip', async () => {

        const created = await db.artifact.cmd.create({
            title: `design-${Date.now()}.png`,
            description: 'mockup',
            filepath: 'designs/mock.png',
        });

        expect(created.artifact_id).toBeGreaterThan(0);

        const fetched = await db.artifact.qry.findById(created.artifact_id);
        expect(fetched?.title).toMatch(/^design-/);
        expect(fetched?.relevance_status).toBe('active');

    });

    it('db.artifact.qry.list returns active artifacts', async () => {

        await db.artifact.cmd.create({ title: `a1-${Date.now()}` });
        await db.artifact.cmd.create({ title: `a2-${Date.now()}` });

        const rows = await db.artifact.qry.list();
        expect(rows.length).toBeGreaterThanOrEqual(2);
        for (const r of rows) {

            expect(r.relevance_status).toBe('active');
        }

    });

    it('db.artifact.cmd.update modifies metadata', async () => {

        const { artifact_id } = await db.artifact.cmd.create({
            title: `upd-${Date.now()}`,
        });

        await db.artifact.cmd.update({
            artifactId: artifact_id,
            title: 'renamed',
            description: 'updated',
            filepath: 'new/path',
        });

        const after = await db.artifact.qry.findById(artifact_id);
        expect(after?.title).toBe('renamed');
        expect(after?.description).toBe('updated');
        expect(after?.filepath).toBe('new/path');

    });

});

describe('artifact: state machine', () => {

    it('db.artifact.cmd.setRelevance moves active -> needs-review', async () => {

        const agent = await db.agent.cmd.create({
            name: `art-agent-${Date.now()}`,
        });
        const { artifact_id } = await db.artifact.cmd.create({
            title: `sm-${Date.now()}`,
        });

        await db.artifact.cmd.setRelevance({
            artifactId: artifact_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
            reason: 'review queue',
        });

        const after = await db.artifact.qry.findById(artifact_id);
        expect(after?.relevance_status).toBe('needs-review');

    });

    it('db.artifact.cmd.delete + restore round-trip', async () => {

        const agent = await db.agent.cmd.create({
            name: `art-life-${Date.now()}`,
        });
        const { artifact_id } = await db.artifact.cmd.create({
            title: `life-${Date.now()}`,
        });

        await db.artifact.cmd.delete({
            artifactId: artifact_id,
            agentId: agent.agent_id,
        });
        const deleted = await db.artifact.qry.findById(artifact_id);
        expect(deleted?.relevance_status).toBe('deleted');

        await db.artifact.cmd.restore({
            artifactId: artifact_id,
            agentId: agent.agent_id,
        });
        const restored = await db.artifact.qry.findById(artifact_id);
        expect(restored?.relevance_status).toBe('active');

    });

});

describe('artifact: attach / detach', () => {

    it('db.artifact.cmd.attachMilestone + detachMilestone are idempotent', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `art-m-${Date.now()}`,
        });
        const { artifact_id } = await db.artifact.cmd.create({
            title: `art-attach-${Date.now()}`,
        });

        await db.artifact.cmd.attachMilestone({
            artifactId: artifact_id,
            milestoneId: milestone.milestone_id,
        });

        // Idempotent: second call must not throw.
        await db.artifact.cmd.attachMilestone({
            artifactId: artifact_id,
            milestoneId: milestone.milestone_id,
        });

        const links = await ctx.kysely
            .selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifact_id)
            .where('milestone_id', '=', milestone.milestone_id)
            .execute();
        expect(links.length).toBe(1);

        await db.artifact.cmd.detachMilestone({
            artifactId: artifact_id,
            milestoneId: milestone.milestone_id,
        });

        const after = await ctx.kysely
            .selectFrom('Milestone_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifact_id)
            .where('milestone_id', '=', milestone.milestone_id)
            .execute();
        expect(after.length).toBe(0);

    });

    it('db.artifact.cmd.attachTask + detachTask are idempotent', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `art-task-m-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'task target',
        });
        const { artifact_id } = await db.artifact.cmd.create({
            title: `art-task-${Date.now()}`,
        });

        await db.artifact.cmd.attachTask({
            artifactId: artifact_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });
        await db.artifact.cmd.attachTask({
            artifactId: artifact_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });

        const links = await ctx.kysely
            .selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifact_id)
            .execute();
        expect(links.length).toBe(1);

        await db.artifact.cmd.detachTask({
            artifactId: artifact_id,
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
        });

        const after = await ctx.kysely
            .selectFrom('Task_Artifact')
            .selectAll()
            .where('artifact_id', '=', artifact_id)
            .execute();
        expect(after.length).toBe(0);

    });

});

describe('artifact: zod boundary', () => {

    it('db.artifact.cmd.create rejects empty title (.min(1))', async () => {

        await expect(db.artifact.cmd.create({ title: '' })).rejects.toThrow();

    });

    it('db.artifact.cmd.attachMilestone rejects negative milestoneId', async () => {

        await expect(db.artifact.cmd.attachMilestone({
            artifactId: 1,
            milestoneId: -1,
        })).rejects.toThrow();

    });

    it('db.artifact.cmd.setRelevance rejects empty newRelevanceStatus', async () => {

        await expect(db.artifact.cmd.setRelevance({
            artifactId: 1,
            newRelevanceStatus: '',
            agentId: 0,
        })).rejects.toThrow();

    });

});
