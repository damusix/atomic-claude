/**
 * Layer 2 — TaskCommands / TaskQueries facade.
 *
 * Composite-key (milestone_id, task_no) ergonomics through the facade,
 * the tracking state machine, depend/undepend graph edges, the bulk-depend
 * TVP path, and the wouldCycle scalar function helper.
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

describe('task: lifecycle', () => {

    it('db.task.cmd.create + qry.findById round-trip with composite PK', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `task-life-${Date.now()}`,
        });

        const created = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'wire it up',
            content: 'design + implement',
        });

        expect(created.milestone_id).toBe(milestone.milestone_id);
        expect(created.task_no).toBeGreaterThan(0);

        const fetched = await db.task.qry.findById(created.milestone_id, created.task_no);
        expect(fetched?.title).toBe('wire it up');
        expect(fetched?.tracking_status).toBe('not-started');

    });

    it('db.task.qry.listByMilestone returns tasks in task_no order', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `list-${Date.now()}`,
        });

        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'first',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'second',
        });
        const c = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'third',
        });

        const rows = await db.task.qry.listByMilestone(milestone.milestone_id);
        expect(rows.length).toBe(3);
        expect(rows[0]?.task_no).toBe(a.task_no);
        expect(rows[1]?.task_no).toBe(b.task_no);
        expect(rows[2]?.task_no).toBe(c.task_no);

    });

    it('db.task.qry.nextTaskNo returns the next assignable number', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `next-${Date.now()}`,
        });

        const first = await db.task.qry.nextTaskNo({
            milestoneId: milestone.milestone_id,
        });
        expect(first).toBeGreaterThan(0);

        const created = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'consume that no',
        });
        expect(created.task_no).toBe(first);

        const second = await db.task.qry.nextTaskNo({
            milestoneId: milestone.milestone_id,
        });
        expect(second).toBe(first + 1);

    });

    it('db.task.cmd.update modifies metadata', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `upd-${Date.now()}`,
        });
        const t = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'before',
        });

        await db.task.cmd.update({
            milestoneId: t.milestone_id,
            taskNo: t.task_no,
            title: 'after',
            content: 'updated',
        });

        const after = await db.task.qry.findById(t.milestone_id, t.task_no);
        expect(after?.title).toBe('after');
        expect(after?.content).toBe('updated');

    });

});

describe('task: state machine', () => {

    it('db.task.cmd.setTracking moves not-started -> in-progress', async () => {

        const agent = await db.agent.cmd.create({
            name: `t-track-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `t-track-m-${Date.now()}`,
        });
        const t = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'work it',
        });

        await db.task.cmd.setTracking({
            milestoneId: t.milestone_id,
            taskNo: t.task_no,
            newTrackingStatus: 'in-progress',
            agentId: agent.agent_id,
        });

        const after = await db.task.qry.findById(t.milestone_id, t.task_no);
        expect(after?.tracking_status).toBe('in-progress');

    });

    it('db.task.cmd.delete transitions tracking_status to abandoned', async () => {

        const agent = await db.agent.cmd.create({
            name: `t-del-${Date.now()}`,
        });
        const milestone = await db.milestone.cmd.create({
            title: `t-del-m-${Date.now()}`,
        });
        const t = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'cancel me',
        });

        await db.task.cmd.delete({
            milestoneId: t.milestone_id,
            taskNo: t.task_no,
            agentId: agent.agent_id,
        });

        const after = await db.task.qry.findById(t.milestone_id, t.task_no);
        expect(after?.tracking_status).toBe('abandoned');

    });

});

describe('task: dependencies', () => {

    it('db.task.cmd.depend + qry.dependencies round-trip', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `dep-${Date.now()}`,
        });
        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'origin',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'dep',
        });

        await db.task.cmd.depend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
            dependencyVerb: 'requires',
        });

        const deps = await db.task.qry.dependencies(a.milestone_id, a.task_no);
        expect(deps.length).toBe(1);
        expect(deps[0]?.dep_task_no).toBe(b.task_no);
        expect(deps[0]?.dependency_verb).toBe('requires');

        const dependents = await db.task.qry.dependents(b.milestone_id, b.task_no);
        expect(dependents.length).toBe(1);
        expect(dependents[0]?.task_no).toBe(a.task_no);

    });

    it('db.task.cmd.undepend removes the edge', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `undep-${Date.now()}`,
        });
        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'origin',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'dep',
        });

        await db.task.cmd.depend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
            dependencyVerb: 'requires',
        });

        await db.task.cmd.undepend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
        });

        const deps = await db.task.qry.dependencies(a.milestone_id, a.task_no);
        expect(deps.length).toBe(0);

    });

    it('db.task.cmd.bulkDepend inserts every supplied dep row', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `bulk-dep-${Date.now()}`,
        });
        const origin = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'origin',
        });

        const targets: { milestone_id: number; task_no: number }[] = [];
        for (let i = 0; i < 3; i += 1) {

            const t = await db.task.cmd.create({
                milestoneId: milestone.milestone_id,
                title: `target-${i}`,
            });
            targets.push(t);
        }

        await db.task.cmd.bulkDepend({
            deps: targets.map((t) => ({
                milestoneId: origin.milestone_id,
                taskNo: origin.task_no,
                depMilestoneId: t.milestone_id,
                depTaskNo: t.task_no,
                dependencyVerb: 'requires',
            })),
        });

        const deps = await db.task.qry.dependencies(origin.milestone_id, origin.task_no);
        expect(deps.length).toBe(3);
        const depTaskNos = deps.map((d) => d.dep_task_no).sort((a, b) => a - b);
        const expected = targets.map((t) => t.task_no).sort((a, b) => a - b);
        expect(depTaskNos).toEqual(expected);

    });

    it('db.task.qry.wouldCycle returns true when a back-edge would close a cycle', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `cycle-${Date.now()}`,
        });
        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'A',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'B',
        });

        // Edge A -> B exists. Inserting B -> A would close the cycle.
        await db.task.cmd.depend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
            dependencyVerb: 'requires',
        });

        const closesCycle = await db.task.qry.wouldCycle({
            milestoneId: b.milestone_id,
            taskNo: b.task_no,
            depMilestoneId: a.milestone_id,
            depTaskNo: a.task_no,
        });
        expect(closesCycle).toBe(true);

        const safe = await db.task.qry.wouldCycle({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
        });
        expect(safe).toBe(false);

    });

});

describe('task: cycle rejection (write-side)', () => {

    it('db.task.cmd.depend rejects an edge that would close an existing cycle', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `cmd-cycle-${Date.now()}`,
        });
        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'A',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'B',
        });

        // Edge A -> B is fine; B -> A would close the cycle and must reject.
        await db.task.cmd.depend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
            dependencyVerb: 'requires',
        });

        await expect(db.task.cmd.depend({
            milestoneId: b.milestone_id,
            taskNo: b.task_no,
            depMilestoneId: a.milestone_id,
            depTaskNo: a.task_no,
            dependencyVerb: 'requires',
        })).rejects.toThrow(/cycle|circular/i);

        // The edge must not have been inserted.
        const reverseDeps = await db.task.qry.dependencies(b.milestone_id, b.task_no);
        expect(reverseDeps.length).toBe(0);

    });

    it('db.task.cmd.bulkDepend rejects a batch whose rows would each form a cycle against the current graph', async () => {

        // sp_Task_Bulk_Depend's cycle check runs against the CURRENT graph,
        // not the would-be-updated one — see
        // sql/09_procedures/12_bulk_tvp_03_sp_Task_Bulk_Depend.sql. We seed
        // an A->B edge, then submit a batch containing B->A so a single row
        // is independently a cycle. The batch must reject and leave the
        // graph untouched.
        const milestone = await db.milestone.cmd.create({
            title: `cmd-bulk-cycle-${Date.now()}`,
        });
        const a = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'A',
        });
        const b = await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'B',
        });

        await db.task.cmd.depend({
            milestoneId: a.milestone_id,
            taskNo: a.task_no,
            depMilestoneId: b.milestone_id,
            depTaskNo: b.task_no,
            dependencyVerb: 'requires',
        });

        await expect(db.task.cmd.bulkDepend({
            deps: [{
                milestoneId: b.milestone_id,
                taskNo: b.task_no,
                depMilestoneId: a.milestone_id,
                depTaskNo: a.task_no,
                dependencyVerb: 'requires',
            }],
        })).rejects.toThrow(/cycle|circular/i);

        const reverseDeps = await db.task.qry.dependencies(b.milestone_id, b.task_no);
        expect(reverseDeps.length).toBe(0);

    });

});

describe('task: backlog', () => {

    it('db.task.qry.backlog returns open tasks under active milestones', async () => {

        const milestone = await db.milestone.cmd.create({
            title: `bk-${Date.now()}`,
        });
        await db.task.cmd.create({
            milestoneId: milestone.milestone_id,
            title: 'open',
        });

        const rows = await db.task.qry.backlog();
        expect(rows.some((r) => r.milestone_id === milestone.milestone_id)).toBe(true);

    });

});

describe('task: zod boundary', () => {

    it('db.task.cmd.create rejects negative milestoneId', async () => {

        await expect(db.task.cmd.create({
            milestoneId: -1,
            title: 'bad',
        })).rejects.toThrow();

    });

    it('db.task.cmd.create rejects empty title (.min(1))', async () => {

        await expect(db.task.cmd.create({
            milestoneId: 1,
            title: '',
        })).rejects.toThrow();

    });

    it('db.task.cmd.bulkDepend rejects empty deps (.min(1))', async () => {

        await expect(db.task.cmd.bulkDepend({ deps: [] })).rejects.toThrow();

    });

    it('db.task.cmd.depend rejects empty dependencyVerb', async () => {

        await expect(db.task.cmd.depend({
            milestoneId: 1,
            taskNo: 1,
            depMilestoneId: 1,
            depTaskNo: 2,
            dependencyVerb: '',
        })).rejects.toThrow();

    });

    it('db.task.qry.wouldCycle rejects negative taskNo', async () => {

        await expect(db.task.qry.wouldCycle({
            milestoneId: 1,
            taskNo: -1,
            depMilestoneId: 1,
            depTaskNo: 2,
        })).rejects.toThrow();

    });

});
