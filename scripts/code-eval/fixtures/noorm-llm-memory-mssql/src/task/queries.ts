/**
 * DQL for the Task domain.
 *
 * Read paths use Kysely directly so callers can project their preferred
 * column set; the helper methods cover the common cases (single Task,
 * Tasks under a Milestone, the open-backlog view, dependency edges).
 *
 * Two function helpers wrap fn_NextTaskNo and fn_TaskDependencyWouldCycle.
 * The cycle helper normalises the BIT result to a JS boolean — drivers
 * surface BIT as either boolean or 0/1, and callers shouldn't have to
 * know which.
 *
 * @example
 * const tasks = new TaskQueries(ctx);
 * const open = await tasks.backlog();
 * const cycles = await tasks.wouldCycle({
 *     milestoneId: 1, taskNo: 2, depMilestoneId: 1, depTaskNo: 3,
 * });
 */
import { Repo } from '../core/repo';

import {
    NextTaskNoInput,
    WouldCycleInput,
} from './schema';

export class TaskQueries extends Repo {

    /** Fetch a single Task by composite PK, or undefined when no row matches. */
    async findById(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .executeTakeFirst();

    }

    /** List every Task under a Milestone, ordered by task_no ascending. */
    async listByMilestone(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .orderBy('task_no', 'asc')
            .execute();

    }

    /** vw_Task_Backlog — open Tasks under active Milestones with is_blocked enrichment. */
    async backlog() {

        return this.ctx.kysely
            .selectFrom('vw_Task_Backlog')
            .selectAll()
            .orderBy('milestone_id', 'asc')
            .orderBy('task_no', 'asc')
            .execute();

    }

    /** Outgoing dependency edges from a Task (this task depends on others). */
    async dependencies(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .execute();

    }

    /** Incoming dependency edges to a Task (others depend on this task). */
    async dependents(depMilestoneId: number, depTaskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('dep_milestone_id', '=', depMilestoneId)
            .where('dep_task_no', '=', depTaskNo)
            .execute();

    }

    /** Return the next task_no that would be assigned for the given Milestone. */
    async nextTaskNo(input: unknown): Promise<number> {

        const { milestoneId } = NextTaskNoInput.parse(input);

        const result = await this.ctx.func('fn_NextTaskNo', {
            milestone_id: milestoneId,
        }, 'next');

        return result.next;

    }

    /**
     * True when inserting (origin -> dep) would close a cycle in the
     * existing dependency graph. Normalises BIT (boolean | 0 | 1) to a
     * JS boolean so callers don't depend on driver-specific surfacing.
     */
    async wouldCycle(input: unknown): Promise<boolean> {

        const { milestoneId, taskNo, depMilestoneId, depTaskNo } = WouldCycleInput.parse(input);

        const result = await this.ctx.func('fn_TaskDependencyWouldCycle', {
            milestone_id: milestoneId,
            task_no: taskNo,
            dep_milestone_id: depMilestoneId,
            dep_task_no: depTaskNo,
        }, 'cycles');

        return result.cycles === true || result.cycles === 1;

    }

}
