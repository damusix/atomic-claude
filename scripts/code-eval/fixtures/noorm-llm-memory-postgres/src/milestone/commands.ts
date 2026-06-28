import { Repo } from '../core/repo';

import {
    CreateMilestoneInput,
    UpdateMilestoneInput,
    SetTrackingInput,
    SetRelevanceInput,
    DeleteMilestoneInput,
    RestoreMilestoneInput,
    CloseMilestoneInput,
    AttachMilestoneToProjectInput,
    DetachMilestoneFromProjectInput,
} from './schema';

/**
 * Write-side API for the `Milestone` table.
 *
 * Every mutation is funneled through the matching `sp_Milestone_*` proc so
 * the transition gates (`fn_IsTrackingTransitionAllowed`,
 * `fn_IsRelevanceTransitionAllowed`), the audit log inserts into
 * `StateTransition` / `Milestone_StateTransition`, and the cascading
 * close-and-abandon batch all stay authoritative in SQL. Methods accept
 * `unknown` and validate at the boundary with Zod; downstream code reads the
 * parsed, camelCase result.
 *
 * @example
 * ```typescript
 * const milestoneId = await db.milestone.cmd.create({
 *     title: 'Q1 launch', provenanceId: 1, agentId: 1,
 * });
 * await db.milestone.cmd.setTracking({
 *     milestoneId, newTrackingStatus: 'in-progress',
 *     agentId: 1, reason: 'work started',
 * });
 * await db.milestone.cmd.close({ milestoneId, agentId: 1, reason: 'shipped' });
 * ```
 */
export class MilestoneCommands extends Repo {

    /**
     * Open a new Milestone and return its generated id. The proc seeds
     * `tracking_status = 'not-started'` and `relevance_status = 'active'`.
     *
     * @example
     * ```typescript
     * const milestoneId = await db.milestone.cmd.create({
     *     title: 'Q1 launch', content: '', reason: 'kickoff',
     *     provenanceId: 1, agentId: 1,
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateMilestoneInput.parse(input);

        const [row] = await this.ctx.proc('sp_Milestone_Create', {
            p_title:         args.title,
            p_content:       args.content,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
        });

        if (!row) throw new Error('sp_Milestone_Create returned no rows');

        return row.milestone_id;

    }

    /**
     * Overwrite a Milestone's text columns. Status changes are not accepted
     * here — use `setTracking` / `setRelevance` so the transition is
     * validated and logged.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.update({
     *     milestoneId: 1, title: 'Q1 launch (revised)',
     *     content: '...', reason: 'scope cut',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateMilestoneInput.parse(input);

        await this.ctx.proc('sp_Milestone_Update', {
            p_milestone_id: args.milestoneId,
            p_title:        args.title,
            p_content:      args.content,
            p_reason:       args.reason,
        });

    }

    /**
     * Move the Milestone to a new `tracking_status`. The proc validates the
     * (from, to) pair against `TrackingStatus_Allowed` and writes a
     * `'milestone-tracking'` row into `StateTransition` /
     * `Milestone_StateTransition` in the same transaction.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.setTracking({
     *     milestoneId: 1, newTrackingStatus: 'in-progress',
     *     agentId: 1, reason: 'work started',
     * });
     * ```
     */
    async setTracking(input: unknown): Promise<void> {

        const args = SetTrackingInput.parse(input);

        await this.ctx.proc('sp_Milestone_SetTracking', {
            p_milestone_id:        args.milestoneId,
            p_new_tracking_status: args.newTrackingStatus,
            p_agent_id:            args.agentId,
            p_reason:              args.reason,
        });

    }

    /**
     * Move the Milestone to a new `relevance_status`. The proc validates the
     * (from, to) pair against `RelevanceStatus_Allowed` and writes a
     * `'milestone-relevance'` row into `StateTransition` /
     * `Milestone_StateTransition` in the same transaction.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.setRelevance({
     *     milestoneId: 1, newRelevanceStatus: 'superseded',
     *     agentId: 1, reason: 'rolled into next quarter',
     * });
     * ```
     */
    async setRelevance(input: unknown): Promise<void> {

        const args = SetRelevanceInput.parse(input);

        await this.ctx.proc('sp_Milestone_SetRelevance', {
            p_milestone_id:         args.milestoneId,
            p_new_relevance_status: args.newRelevanceStatus,
            p_agent_id:             args.agentId,
            p_reason:               args.reason,
        });

    }

    /**
     * Soft-delete the Milestone (relevance → `'deleted'`) and cascade into
     * Notes attached via `Milestone_Note` and `Task_Note`. Child Tasks and
     * join rows are hidden by the parent's soft delete; `sp_Cleanup`
     * hard-deletes everything via FK CASCADE past the TTL. Named
     * `softDelete` only to avoid `delete` as a method name in TS.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.softDelete({
     *     milestoneId: 1, agentId: 1, reason: 'duplicate',
     * });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteMilestoneInput.parse(input);

        await this.ctx.proc('sp_Milestone_Delete', {
            p_milestone_id: args.milestoneId,
            p_agent_id:     args.agentId,
            p_reason:       args.reason,
        });

    }

    /**
     * Reverse a previous `softDelete` by routing through SetRelevance with
     * `'active'`. Rejected if the milestone isn't currently `'deleted'`.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.restore({
     *     milestoneId: 1, agentId: 1, reason: 'restore — still relevant',
     * });
     * ```
     */
    async restore(input: unknown): Promise<void> {

        const args = RestoreMilestoneInput.parse(input);

        await this.ctx.proc('sp_Milestone_Restore', {
            p_milestone_id: args.milestoneId,
            p_agent_id:     args.agentId,
            p_reason:       args.reason,
        });

    }

    /**
     * Wrap up a Milestone in one transactional batch: tracking → `'done'`,
     * relevance → `'superseded'`, plus tracking → `'abandoned'` for every
     * still-open Task underneath. Any rejected transition rolls back the
     * whole close.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.close({
     *     milestoneId: 1, agentId: 1, reason: 'shipped',
     * });
     * ```
     */
    async close(input: unknown): Promise<void> {

        const args = CloseMilestoneInput.parse(input);

        await this.ctx.proc('sp_Milestone_Close', {
            p_milestone_id: args.milestoneId,
            p_agent_id:     args.agentId,
            p_reason:       args.reason,
        });

    }

    /**
     * Attach a Milestone to a Project (insert into `Project_Milestone`).
     * Idempotent at the SQL layer — re-attaching the same pair is a no-op.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.attachProject({ milestoneId: 1, projectId: 2 });
     * ```
     */
    async attachProject(input: unknown): Promise<void> {

        const args = AttachMilestoneToProjectInput.parse(input);

        await this.ctx.proc('sp_Milestone_Attach_Project', {
            p_milestone_id: args.milestoneId,
            p_project_id:   args.projectId,
        });

    }

    /**
     * Detach a Milestone from a Project (delete from `Project_Milestone`).
     * Idempotent — detaching a pair that doesn't exist is a no-op.
     *
     * @example
     * ```typescript
     * await db.milestone.cmd.detachProject({ milestoneId: 1, projectId: 2 });
     * ```
     */
    async detachProject(input: unknown): Promise<void> {

        const args = DetachMilestoneFromProjectInput.parse(input);

        await this.ctx.proc('sp_Milestone_Detach_Project', {
            p_milestone_id: args.milestoneId,
            p_project_id:   args.projectId,
        });

    }

}
