/**
 * DML for the Task domain.
 *
 * Each method validates input with a Zod schema, then dispatches to the
 * matching stored procedure. The state-machine guard, cycle check, and
 * dependency-verb / tracking_status validation all live in the procs —
 * we let RAISERROR propagate as a tedious error instead of duplicating
 * the rules here.
 *
 * @example
 * const tasks = new TaskCommands(ctx);
 * const { milestone_id, task_no } = await tasks.create({
 *     milestoneId: 5,
 *     title: 'Wire up bulk depend',
 * });
 */
import { tvp } from '@noormdev/sdk';

import { Repo } from '../core/repo';

import {
    BulkDependInput,
    CreateTaskInput,
    DeleteTaskInput,
    DependInput,
    SetTrackingInput,
    UndependInput,
    UpdateTaskInput,
} from './schema';

export class TaskCommands extends Repo {

    /** Insert a Task under the given Milestone; task_no is auto-assigned via fn_NextTaskNo. */
    async create(input: unknown): Promise<{ milestone_id: number; task_no: number }> {

        const parsed = CreateTaskInput.parse(input);

        const rows = await this.ctx.proc('sp_Task_Create', {
            milestone_id: parsed.milestoneId,
            title: parsed.title,
            content: parsed.content,
            reason: parsed.reason,
            agent_id: parsed.agentId,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Task_Create returned no rows.');

        return row;

    }

    /** Update Task metadata (title/content/reason). Tracking moves go through setTracking. */
    async update(input: unknown) {

        const parsed = UpdateTaskInput.parse(input);

        return this.ctx.proc('sp_Task_Update', {
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
            title: parsed.title,
            content: parsed.content,
            reason: parsed.reason,
        });

    }

    /** Drive the tracking state machine; proc validates the edge and writes a StateTransition. */
    async setTracking(input: unknown) {

        const parsed = SetTrackingInput.parse(input);

        return this.ctx.proc('sp_Task_SetTracking', {
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
            new_tracking_status: parsed.newTrackingStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-delete: cascade-delete attached Notes, then transition tracking to 'abandoned'. */
    async delete(input: unknown) {

        const parsed = DeleteTaskInput.parse(input);

        return this.ctx.proc('sp_Task_Delete', {
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Add a single dependency edge; rejects self-refs, unknown verbs, and would-be cycles. */
    async depend(input: unknown) {

        const parsed = DependInput.parse(input);

        return this.ctx.proc('sp_Task_Depend', {
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
            dep_milestone_id: parsed.depMilestoneId,
            dep_task_no: parsed.depTaskNo,
            dependency_verb: parsed.dependencyVerb,
            reason: parsed.reason,
        });

    }

    /** Idempotent edge removal — silent no-op when the row was never present. */
    async undepend(input: unknown) {

        const parsed = UndependInput.parse(input);

        return this.ctx.proc('sp_Task_Undepend', {
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
            dep_milestone_id: parsed.depMilestoneId,
            dep_task_no: parsed.depTaskNo,
        });

    }

    /**
     * Bulk dependency insert via TaskDependencyInput TVP. The proc rejects
     * the entire batch on any self-ref, unknown verb, or single-row cycle
     * detected against the current graph; combined-rows cycles are not
     * caught — use depend() per row when that matters.
     */
    async bulkDepend(input: unknown): Promise<void> {

        const { deps } = BulkDependInput.parse(input);

        await this.ctx.proc('sp_Task_Bulk_Depend', {
            Deps: tvp('TaskDependencyInput', deps.map((d) => ({
                milestone_id: d.milestoneId,
                task_no: d.taskNo,
                dep_milestone_id: d.depMilestoneId,
                dep_task_no: d.depTaskNo,
                dependency_verb: d.dependencyVerb,
                reason: d.reason ?? '',
            }))),
        });

    }

}
