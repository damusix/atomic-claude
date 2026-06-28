/**
 * Layer 2 — MilestoneCommands / MilestoneQueries facade.
 *
 * Two state machines (tracking + relevance) plus the close-cascade.
 * Also covers vw_Milestone_Stats and the project-attach pair.
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

describe('milestone: lifecycle', () => {

    it('db.milestone.cmd.create + qry.findById round-trip', async () => {

        const created = await db.milestone.cmd.create({
            title: `v2 launch ${Date.now()}`,
            content: 'ship the next major',
        });

        expect(created.milestone_id).toBeGreaterThan(0);

        const fetched = await db.milestone.qry.findById(created.milestone_id);
        expect(fetched?.title).toMatch(/^v2 launch/);
        expect(fetched?.tracking_status).toBe('not-started');
        expect(fetched?.relevance_status).toBe('active');

    });

    it('db.milestone.qry.list returns active milestones', async () => {

        await db.milestone.cmd.create({ title: `m1-${Date.now()}` });
        await db.milestone.cmd.create({ title: `m2-${Date.now()}` });

        const rows = await db.milestone.qry.list();
        expect(rows.length).toBeGreaterThanOrEqual(2);
        for (const r of rows) {

            expect(r.relevance_status).toBe('active');
        }

    });

    it('db.milestone.cmd.update modifies metadata', async () => {

        const { milestone_id } = await db.milestone.cmd.create({
            title: `before-${Date.now()}`,
        });

        await db.milestone.cmd.update({
            milestoneId: milestone_id,
            title: 'after',
            content: 'updated',
        });

        const after = await db.milestone.qry.findById(milestone_id);
        expect(after?.title).toBe('after');
        expect(after?.content).toBe('updated');

    });

});

describe('milestone: state machines', () => {

    it('db.milestone.cmd.setTracking moves not-started -> in-progress', async () => {

        const agent = await db.agent.cmd.create({
            name: `m-track-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `track-${Date.now()}`,
        });

        await db.milestone.cmd.setTracking({
            milestoneId: milestone_id,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });

        const after = await db.milestone.qry.findById(milestone_id);
        expect(after?.tracking_status).toBe('in-progress');

    });

    it('db.milestone.cmd.setRelevance moves active -> needs-review', async () => {

        const agent = await db.agent.cmd.create({
            name: `m-rel-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `rel-${Date.now()}`,
        });

        await db.milestone.cmd.setRelevance({
            milestoneId: milestone_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });

        const after = await db.milestone.qry.findById(milestone_id);
        expect(after?.relevance_status).toBe('needs-review');

    });

    it('db.milestone.cmd.delete + restore round-trip', async () => {

        const agent = await db.agent.cmd.create({
            name: `m-life-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `life-${Date.now()}`,
        });

        await db.milestone.cmd.delete({
            milestoneId: milestone_id,
            agentId: agent.agent_id,
        });
        const deleted = await db.milestone.qry.findById(milestone_id);
        expect(deleted?.relevance_status).toBe('deleted');

        await db.milestone.cmd.restore({
            milestoneId: milestone_id,
            agentId: agent.agent_id,
        });
        const restored = await db.milestone.qry.findById(milestone_id);
        expect(restored?.relevance_status).toBe('active');

    });

});

describe('milestone: close cascade', () => {

    it('db.milestone.cmd.close sets tracking=done, relevance=superseded, abandons open child Tasks', async () => {

        const agent = await db.agent.cmd.create({
            name: `close-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `close-${Date.now()}`,
        });

        const taskA = await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'open task A',
        });
        const taskB = await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'open task B',
        });

        // Move milestone to in-progress before closing — TrackingStatus_Allowed
        // does not contain 'not-started' -> 'done' (only 'in-progress' -> 'done').
        // sp_Milestone_Close calls SetTracking('done') directly per the artifact.
        await db.milestone.cmd.setTracking({
            milestoneId: milestone_id,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
            reason: 'starting work before close',
        });

        await db.milestone.cmd.close({
            milestoneId: milestone_id,
            agentId: agent.agent_id,
            reason: 'wrap up',
        });

        const after = await db.milestone.qry.findById(milestone_id);
        expect(after?.tracking_status).toBe('done');
        expect(after?.relevance_status).toBe('superseded');

        const tA = await db.task.qry.findById(taskA.milestone_id, taskA.task_no);
        const tB = await db.task.qry.findById(taskB.milestone_id, taskB.task_no);
        expect(tA?.tracking_status).toBe('abandoned');
        expect(tB?.tracking_status).toBe('abandoned');

    });

});

describe('milestone: stats + project link', () => {

    it('db.milestone.qry.stats returns rollup row from vw_Milestone_Stats', async () => {

        const { milestone_id } = await db.milestone.cmd.create({
            title: `stats-${Date.now()}`,
        });

        await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'one',
        });
        await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'two',
        });

        const stats = await db.milestone.qry.stats(milestone_id);
        expect(stats?.milestone_id).toBe(milestone_id);

    });

    it('db.milestone.qry.stats rollup columns aggregate per-status task counts', async () => {

        const agent = await db.agent.cmd.create({
            name: `stats-rollup-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `stats-rollup-${Date.now()}`,
        });

        // Three tasks, three different tracking_status values driven by
        // sp_Task_SetTracking. Allowed edges (TrackingStatus_Allowed):
        //   not-started -> in-progress, not-started -> abandoned,
        //   in-progress -> done.
        const tInProgress = await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'in-progress',
        });
        const tDone = await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'done',
        });
        const tAbandoned = await db.task.cmd.create({
            milestoneId: milestone_id,
            title: 'abandoned',
        });

        await db.task.cmd.setTracking({
            milestoneId: tInProgress.milestone_id,
            taskNo: tInProgress.task_no,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });
        await db.task.cmd.setTracking({
            milestoneId: tDone.milestone_id,
            taskNo: tDone.task_no,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });
        await db.task.cmd.setTracking({
            milestoneId: tDone.milestone_id,
            taskNo: tDone.task_no,
            newTrackingStatus: 'done',
            agentId: agent.agent_id,
        });
        await db.task.cmd.setTracking({
            milestoneId: tAbandoned.milestone_id,
            taskNo: tAbandoned.task_no,
            newTrackingStatus: 'abandoned',
            agentId: agent.agent_id,
        });

        const stats = await db.milestone.qry.stats(milestone_id);
        expect(stats?.milestone_id).toBe(milestone_id);
        expect(stats?.total_tasks).toBe(3);
        expect(stats?.done_tasks).toBe(1);
        expect(stats?.abandoned_tasks).toBe(1);
        // open_tasks per fn_IsOpen: 'in-progress' counts as open;
        // 'done' and 'abandoned' do not.
        expect(stats?.open_tasks).toBe(1);
        // No tags / artifacts / notes / dependencies / projects attached.
        expect(stats?.total_tags).toBe(0);
        expect(stats?.total_artifacts).toBe(0);
        expect(stats?.total_notes).toBe(0);
        expect(stats?.total_dependencies).toBe(0);
        expect(stats?.project_count).toBe(0);
        expect(stats?.blocked_tasks).toBe(0);

    });

    it('db.milestone.cmd.attachProject + detachProject round-trip', async () => {

        const project = await db.project.cmd.create({
            name: `attach-${Date.now()}`,
        });
        const { milestone_id } = await db.milestone.cmd.create({
            title: `attach-m-${Date.now()}`,
        });

        await db.milestone.cmd.attachProject({
            milestoneId: milestone_id,
            projectId: project.project_id,
        });

        const linked = await ctx.kysely
            .selectFrom('Project_Milestone')
            .selectAll()
            .where('milestone_id', '=', milestone_id)
            .where('project_id', '=', project.project_id)
            .execute();
        expect(linked.length).toBe(1);

        await db.milestone.cmd.detachProject({
            milestoneId: milestone_id,
            projectId: project.project_id,
        });

        const after = await ctx.kysely
            .selectFrom('Project_Milestone')
            .selectAll()
            .where('milestone_id', '=', milestone_id)
            .where('project_id', '=', project.project_id)
            .execute();
        expect(after.length).toBe(0);

    });

});

describe('milestone: zod boundary', () => {

    it('db.milestone.cmd.create rejects empty title (.min(1))', async () => {

        await expect(db.milestone.cmd.create({ title: '' })).rejects.toThrow();

    });

    it('db.milestone.cmd.setTracking rejects empty newTrackingStatus', async () => {

        await expect(db.milestone.cmd.setTracking({
            milestoneId: 1,
            newTrackingStatus: '',
            agentId: 0,
        })).rejects.toThrow();

    });

    it('db.milestone.cmd.attachProject rejects negative projectId', async () => {

        await expect(db.milestone.cmd.attachProject({
            milestoneId: 1,
            projectId: -1,
        })).rejects.toThrow();

    });

});
