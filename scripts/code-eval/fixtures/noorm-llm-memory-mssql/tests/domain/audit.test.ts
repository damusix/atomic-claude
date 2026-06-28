/**
 * Layer 2 — AuditQueries facade.
 *
 * Audit is read-only. Every state-machine proc writes a StateTransition
 * row + matching subtype row; vw_StateTransition / vw_Recent_Activity /
 * vw_Agent_Activity expose them. These tests trigger transitions through
 * the facade and assert the audit views surface them.
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

describe('audit: per-entity transitions', () => {

    it('db.audit.qry.transitions returns Memory relevance moves', async () => {

        const agent = await db.agent.cmd.create({
            name: `audit-mem-${Date.now()}`,
        });
        const { memory_id } = await db.memory.cmd.create({
            content: 'audit me',
            domain: 'backend',
            category: 'fact',
        });

        await db.memory.cmd.setRelevance({
            memoryId: memory_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
            reason: 'queue review',
        });
        await db.memory.cmd.delete({
            memoryId: memory_id,
            agentId: agent.agent_id,
        });

        const rows = await db.audit.qry.transitions({
            entityType: 'memory',
            entityId: memory_id,
        });

        expect(rows.length).toBe(2);
        // Newest first.
        expect(rows[0]?.to_status).toBe('deleted');
        expect(rows[1]?.to_status).toBe('needs-review');
        for (const r of rows) {

            expect(r.state_transition_type).toBe('memory-relevance');
            expect(r.memory_id).toBe(memory_id);
        }

    });

    it('db.audit.qry.transitions returns Note transitions', async () => {

        const agent = await db.agent.cmd.create({
            name: `audit-note-${Date.now()}`,
        });
        const project = await db.project.cmd.create({
            name: `audit-note-prj-${Date.now()}`,
        });
        const { note_id } = await db.note.cmd.createForProject({
            projectId: project.project_id,
            content: 'audit',
        });

        await db.note.cmd.setRelevance({
            noteId: note_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });

        const rows = await db.audit.qry.transitions({
            entityType: 'note',
            entityId: note_id,
        });

        expect(rows.length).toBe(1);
        expect(rows[0]?.note_id).toBe(note_id);
        expect(rows[0]?.to_status).toBe('needs-review');

    });

    it('db.audit.qry.transitions returns Task tracking moves with composite key', async () => {

        const agent = await db.agent.cmd.create({
            name: `audit-task-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `audit-task-m-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'audit task',
        });

        await db.task.cmd.setTracking({
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });

        const rows = await db.audit.qry.transitions({
            entityType: 'task',
            entityId: task.task_no,
            milestoneId: task.milestone_id,
        });

        expect(rows.length).toBe(1);
        expect(rows[0]?.task_no).toBe(task.task_no);
        expect(rows[0]?.milestone_id).toBe(task.milestone_id);
        expect(rows[0]?.to_status).toBe('in-progress');

    });

    it('db.audit.qry.transitions throws when entityType=task is missing milestoneId', async () => {

        await expect(db.audit.qry.transitions({
            entityType: 'task',
            entityId: 1,
        })).rejects.toThrow();

    });

});

describe('audit: recentActivity', () => {

    it('db.audit.qry.recentActivity returns rows newest-first after creates + transitions', async () => {

        const agent = await db.agent.cmd.create({
            name: `recent-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `recent-m-${Date.now()}`,
        });
        const memory = await db.memory.cmd.create({
            content: 'recent memory',
            domain: 'backend',
            category: 'fact',
        });

        await db.milestone.cmd.setTracking({
            milestoneId: milestone.milestone_id,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });
        await db.memory.cmd.setRelevance({
            memoryId: memory.memory_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });

        const recent = await db.audit.qry.recentActivity(10);

        expect(recent.length).toBeGreaterThanOrEqual(1);

        for (let i = 1; i < recent.length; i += 1) {

            const prev = recent[i - 1]?.occurred_at;
            const curr = recent[i]?.occurred_at;
            if (prev && curr) {

                expect(prev.getTime()).toBeGreaterThanOrEqual(curr.getTime());
            }
        }

    });

    it('db.audit.qry.recentActivity respects the limit', async () => {

        const agent = await db.agent.cmd.create({
            name: `limit-${Date.now()}`,
        });
        const memory = await db.memory.cmd.create({
            content: 'limit', domain: 'backend', category: 'fact',
        });
        await db.memory.cmd.setRelevance({
            memoryId: memory.memory_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });
        await db.memory.cmd.setRelevance({
            memoryId: memory.memory_id,
            newRelevanceStatus: 'active',
            agentId: agent.agent_id,
        });

        const recent = await db.audit.qry.recentActivity(1);
        expect(recent.length).toBeLessThanOrEqual(1);

    });

});

describe('audit: agentActivity', () => {

    it('db.audit.qry.agentActivity returns counts for the given agent', async () => {

        const agent = await db.agent.cmd.create({
            name: `agent-act-${Date.now()}`,
        });

        const milestone = await db.milestone.cmd.create({
            title: `aa-m-${Date.now()}`,
            agentId: agent.agent_id,
        });
        const memory = await db.memory.cmd.create({
            content: 'aa memory',
            domain: 'backend',
            category: 'fact',
            agentId: agent.agent_id,
        });

        await db.memory.cmd.setRelevance({
            memoryId: memory.memory_id,
            newRelevanceStatus: 'needs-review',
            agentId: agent.agent_id,
        });
        await db.milestone.cmd.setTracking({
            milestoneId: milestone.milestone_id,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });

        const activity = await db.audit.qry.agentActivity(agent.agent_id);

        expect(activity).toBeDefined();
        expect(activity?.agent_id).toBe(agent.agent_id);
        expect(activity?.transitions_made).toBeGreaterThanOrEqual(2);

    });

    it('db.audit.qry.agentActivity returns undefined for an unknown agent', async () => {

        const activity = await db.audit.qry.agentActivity(999_999);
        expect(activity).toBeUndefined();

    });

});

describe('audit: milestoneHistory', () => {

    it('db.audit.qry.milestoneHistory returns the milestone + child task transitions', async () => {

        const agent = await db.agent.cmd.create({
            name: `mhist-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `mhist-${Date.now()}`,
        });
        const task = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'history task',
        });

        await db.milestone.cmd.setTracking({
            milestoneId: milestone.milestone_id,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });
        await db.task.cmd.setTracking({
            milestoneId: task.milestone_id,
            taskNo: task.task_no,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });

        const history = await db.audit.qry.milestoneHistory(milestone.milestone_id);

        expect(history.length).toBeGreaterThanOrEqual(2);

        const types = history.map((r) => r.state_transition_type);
        expect(types).toContain('milestone-tracking');
        expect(types).toContain('task-tracking');

        // Newest-first ordering.
        for (let i = 1; i < history.length; i += 1) {

            const prev = history[i - 1]?.occurred_at;
            const curr = history[i]?.occurred_at;
            if (prev && curr) {

                expect(prev.getTime()).toBeGreaterThanOrEqual(curr.getTime());
            }
        }

    });

});
