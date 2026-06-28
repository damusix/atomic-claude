import { Repo } from '../core/repo';

import {
    ListTaskOpts,
    WouldCycleInput,
} from './schema';

/**
 * Read-side API for the `Task` table, the `vw_Task_Backlog` view, and
 * the two task scalar functions.
 *
 * Reads go straight to the table (or view) via Kysely; scalar functions
 * are invoked through `ctx.func()` and their result column is projected
 * out under the function name (PostgreSQL's default for `RETURNS <scalar>`).
 * List queries are always pagination-bounded (default 50, max 500) to
 * keep accidental "list everything" calls from sweeping large milestones.
 *
 * @example
 * ```typescript
 * const task    = await db.task.qry.findById(7, 3);
 * const open    = await db.task.qry.listForMilestone(7, { openOnly: true });
 * const backlog = await db.task.qry.backlog({ limit: 25 });
 * const next    = await db.task.qry.nextTaskNo(7);
 * const cycle   = await db.task.qry.wouldCycle({
 *     milestoneId: 7, taskNo: 3, depMilestoneId: 7, depTaskNo: 1,
 * });
 * ```
 */
export class TaskQueries extends Repo {

    /**
     * Look up a single task by its composite primary key. Returns
     * `undefined` when no row matches — callers decide whether that's
     * an error or a normal "not yet created" signal.
     *
     * @example
     * ```typescript
     * const task = await db.task.qry.findById(7, 3);
     * if (!task) throw new Error('task not found');
     * ```
     */
    async findById(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .executeTakeFirst();

    }

    /**
     * Paginate tasks under a milestone, ordered by `task_no`. When
     * `openOnly` is `true` (the default), tasks whose `tracking_status`
     * is `'done'` or `'abandoned'` are filtered out — the common
     * "what's left to do under this milestone" view.
     *
     * @example
     * ```typescript
     * const open = await db.task.qry.listForMilestone(7, {});
     * const all  = await db.task.qry.listForMilestone(7, { openOnly: false });
     * ```
     */
    async listForMilestone(milestoneId: number, input: unknown) {

        const opts = ListTaskOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('Task')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .orderBy('task_no', 'asc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.openOnly) {

            q = q.where('tracking_status', 'not in', ['done', 'abandoned']);
        }

        return q.execute();

    }

    /**
     * Paginate the project-wide backlog from `vw_Task_Backlog` — every
     * task whose parent milestone is `'active'` and whose own
     * `tracking_status` is open, with `is_blocked` set when the task
     * has at least one unresolved `'blocks'` dependency. Ordered by
     * `(milestone_id, task_no)` inside the view.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.task.qry.backlog({});
     * const secondPage = await db.task.qry.backlog({ limit: 50, offset: 50 });
     * ```
     */
    async backlog(input: unknown) {

        const opts = ListTaskOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Task_Backlog')
            .selectAll()
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Compute the next `task_no` that `sp_Task_Create` would assign for
     * a given milestone. Wraps `fn_NextTaskNo(milestone_id)` — useful
     * for previewing the id in a planning UI before the row exists.
     *
     * @example
     * ```typescript
     * const next = await db.task.qry.nextTaskNo(7); // 1, 2, 3, ...
     * ```
     */
    async nextTaskNo(milestoneId: number): Promise<number> {

        const result = await this.ctx.func(
            'fn_NextTaskNo',
            { p_milestone_id: milestoneId },
            'fn_NextTaskNo',
        );

        return result.fn_NextTaskNo;

    }

    /**
     * Read-side preview of the cycle check `sp_Task_Depend` runs
     * internally before insert. Wraps `fn_TaskDependencyWouldCycle(...)`
     * so a planning UI can warn ("this would create a cycle") without
     * attempting the mutation.
     *
     * @example
     * ```typescript
     * const wouldCycle = await db.task.qry.wouldCycle({
     *     milestoneId: 7, taskNo: 3, depMilestoneId: 7, depTaskNo: 1,
     * });
     * if (wouldCycle) console.warn('rejecting — would close a cycle');
     * ```
     */
    async wouldCycle(input: unknown): Promise<boolean> {

        const args = WouldCycleInput.parse(input);

        const result = await this.ctx.func(
            'fn_TaskDependencyWouldCycle',
            {
                p_milestone_id:     args.milestoneId,
                p_task_no:          args.taskNo,
                p_dep_milestone_id: args.depMilestoneId,
                p_dep_task_no:      args.depTaskNo,
            },
            'fn_TaskDependencyWouldCycle',
        );

        return result.fn_TaskDependencyWouldCycle;

    }

    /**
     * List the dependency edges where `(milestoneId, taskNo)` is the
     * dependent — i.e. "what does this task depend on?". Returns the
     * raw `Task_Dependency` rows including the verb and reason.
     *
     * @example
     * ```typescript
     * const deps = await db.task.qry.listDependencies(7, 3);
     * for (const d of deps) console.log(d.dep_milestone_id, d.dep_task_no, d.dependency_verb);
     * ```
     */
    async listDependencies(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .where('task_no', '=', taskNo)
            .execute();

    }

    /**
     * Reverse direction of `listDependencies` — every edge where
     * `(milestoneId, taskNo)` is the dependency target, i.e. "what
     * tasks depend on this one?". Useful for impact analysis before
     * marking a task abandoned or rescoping it.
     *
     * @example
     * ```typescript
     * const dependents = await db.task.qry.listDependents(7, 1);
     * for (const d of dependents) console.log(d.milestone_id, d.task_no, d.dependency_verb);
     * ```
     */
    async listDependents(milestoneId: number, taskNo: number) {

        return this.ctx.kysely
            .selectFrom('Task_Dependency')
            .selectAll()
            .where('dep_milestone_id', '=', milestoneId)
            .where('dep_task_no', '=', taskNo)
            .execute();

    }

}
