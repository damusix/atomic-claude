import { Repo } from '../core/repo';

import {
    CreateTaskInput,
    UpdateTaskInput,
    SetTrackingInput,
    DeleteTaskInput,
    DependInput,
    UndependInput,
} from './schema';

/**
 * Write-side API for the `Task` table and its dependency edges.
 *
 * Every mutation flows through the matching `sp_Task_*` proc so the
 * MAX+1 numbering, state-machine guards, audit trail (`Task_StateTransition`),
 * cycle check, and idempotent join writes stay authoritative in SQL.
 * Methods accept `unknown` and validate at the boundary with Zod;
 * downstream code reads the parsed, camelCase result.
 *
 * @example
 * ```typescript
 * const { milestoneId, taskNo } = await db.task.cmd.create({
 *     milestoneId: 7, title: 'Wire DT importer', agentId: 1,
 * });
 *
 * await db.task.cmd.setTracking({
 *     milestoneId, taskNo, newTrackingStatus: 'in-progress',
 *     agentId: 1, reason: 'started spike',
 * });
 *
 * await db.task.cmd.depend({
 *     milestoneId, taskNo, depMilestoneId: 7, depTaskNo: 1,
 *     dependencyVerb: 'blocks', reason: 'needs schema first',
 * });
 * ```
 */
export class TaskCommands extends Repo {

    /**
     * Create a new Task under a milestone and return its composite primary
     * key. `task_no` is computed by `fn_NextTaskNo(milestone_id)` inside
     * the proc (MAX+1 within the milestone), so the returned `taskNo`
     * is the canonical identifier — the caller does not pre-compute it.
     *
     * @example
     * ```typescript
     * const { milestoneId, taskNo } = await db.task.cmd.create({
     *     milestoneId: 7, title: 'Wire DT importer',
     *     content: '', reason: 'planning', agentId: 1,
     * });
     * ```
     */
    async create(input: unknown): Promise<{ milestoneId: number; taskNo: number }> {

        const args = CreateTaskInput.parse(input);

        const [row] = await this.ctx.proc('sp_Task_Create', {
            p_milestone_id: args.milestoneId,
            p_title:        args.title,
            p_content:      args.content,
            p_reason:       args.reason,
            p_agent_id:     args.agentId,
        });

        if (!row) throw new Error('sp_Task_Create returned no rows');

        return {
            milestoneId: row.milestone_id,
            taskNo:      row.task_no,
        };

    }

    /**
     * Overwrite a Task's mutable text columns. The proc rejects when the
     * `(milestone_id, task_no)` tuple does not exist. Tracking-status
     * changes go through `setTracking`; this method does not touch status
     * or audit history.
     *
     * @example
     * ```typescript
     * await db.task.cmd.update({
     *     milestoneId: 7, taskNo: 3,
     *     title: 'Wire DT importer (revised)',
     *     content: 'Use the new bridge API.',
     *     reason: 'scope clarified',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateTaskInput.parse(input);

        await this.ctx.proc('sp_Task_Update', {
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
            p_title:        args.title,
            p_content:      args.content,
            p_reason:       args.reason,
        });

    }

    /**
     * Drive the tracking state machine. The proc looks up the current
     * `tracking_status`, validates the (current, new) pair against
     * `TrackingStatus_Allowed`, applies the change, and writes the
     * audit row to `StateTransition` + `Task_StateTransition`. Rejected
     * transitions raise a SQL exception and roll back.
     *
     * @example
     * ```typescript
     * await db.task.cmd.setTracking({
     *     milestoneId: 7, taskNo: 3, newTrackingStatus: 'in-progress',
     *     agentId: 1, reason: 'started spike',
     * });
     * ```
     */
    async setTracking(input: unknown): Promise<void> {

        const args = SetTrackingInput.parse(input);

        await this.ctx.proc('sp_Task_SetTracking', {
            p_milestone_id:        args.milestoneId,
            p_task_no:             args.taskNo,
            p_new_tracking_status: args.newTrackingStatus,
            p_agent_id:            args.agentId,
            p_reason:              args.reason,
        });

    }

    /**
     * Soft-delete the task: every Note attached via `Task_Note` is
     * transitioned to `relevance_status = 'deleted'` (audited via
     * `Note_StateTransition`), then the task itself is moved to
     * `tracking_status = 'abandoned'` (audited via `Task_StateTransition`).
     * Tasks have no `relevance_status` of their own — they're hidden from
     * "open" lists by their `tracking_status` and from result sets by
     * their parent milestone's `relevance_status`.
     *
     * @example
     * ```typescript
     * await db.task.cmd.softDelete({
     *     milestoneId: 7, taskNo: 3, agentId: 1, reason: 'duplicate of task 5',
     * });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteTaskInput.parse(input);

        await this.ctx.proc('sp_Task_Delete', {
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
            p_agent_id:     args.agentId,
            p_reason:       args.reason,
        });

    }

    /**
     * Record a dependency edge. The proc rejects self-edges and any edge
     * that would close a cycle (validated via `fn_TaskDependencyWouldCycle`
     * before insert), and unknown verbs raise a referential-integrity
     * error. The underlying `INSERT ... ON CONFLICT DO NOTHING` makes
     * re-issuing the same edge a silent no-op.
     *
     * @example
     * ```typescript
     * await db.task.cmd.depend({
     *     milestoneId: 7, taskNo: 3,
     *     depMilestoneId: 7, depTaskNo: 1,
     *     dependencyVerb: 'blocks', reason: 'needs schema first',
     * });
     * ```
     */
    async depend(input: unknown): Promise<void> {

        const args = DependInput.parse(input);

        await this.ctx.proc('sp_Task_Depend', {
            p_milestone_id:     args.milestoneId,
            p_task_no:          args.taskNo,
            p_dep_milestone_id: args.depMilestoneId,
            p_dep_task_no:      args.depTaskNo,
            p_dependency_verb:  args.dependencyVerb,
            p_reason:           args.reason,
        });

    }

    /**
     * Drop the dependency edge identified by the four-key tuple.
     * Idempotent — `DELETE` over a non-existent row is a silent no-op.
     *
     * @example
     * ```typescript
     * await db.task.cmd.undepend({
     *     milestoneId: 7, taskNo: 3, depMilestoneId: 7, depTaskNo: 1,
     * });
     * ```
     */
    async undepend(input: unknown): Promise<void> {

        const args = UndependInput.parse(input);

        await this.ctx.proc('sp_Task_Undepend', {
            p_milestone_id:     args.milestoneId,
            p_task_no:          args.taskNo,
            p_dep_milestone_id: args.depMilestoneId,
            p_dep_task_no:      args.depTaskNo,
        });

    }

}
