import { Repo } from '../core/repo';

import {
    AttachArtifactToMilestoneInput,
    AttachArtifactToTaskInput,
    CreateArtifactInput,
    DeleteArtifactInput,
    DetachArtifactFromMilestoneInput,
    DetachArtifactFromTaskInput,
    RestoreArtifactInput,
    SetRelevanceInput,
    UpdateArtifactInput,
} from './schema';

/**
 * Mutation surface for the `Artifact` domain.
 *
 * One method per `sp_Artifact_*` stored procedure, in the same order they
 * appear in the schema artifact. Each method:
 *
 *   1. Parses untyped input through the matching Zod schema.
 *   2. Maps camelCase fields onto the proc's snake_case args.
 *   3. Awaits the proc and unwraps the return value where useful
 *      (e.g. `create` returns a bare `number` rather than `{ artifact_id }`).
 *
 * Relevance-state changes flow through `setRelevance`, `softDelete`, and
 * `restore` — never through `update` — so the `Artifact_StateTransition`
 * audit row is always written. Attach/Detach methods are idempotent at
 * the SQL layer (`ON CONFLICT DO NOTHING` / plain `DELETE`).
 *
 * @example
 * ```typescript
 * const artifactId = await db.artifact.cmd.create({
 *     title: 'design.md', filepath: 'docs/design.md',
 *     provenanceId: 1, agentId: 1,
 * });
 * await db.artifact.cmd.attachMilestone({ artifactId, milestoneId: 1 });
 * await db.artifact.cmd.softDelete({ artifactId, agentId: 1, reason: 'obsolete' });
 * ```
 */
export class ArtifactCommands extends Repo {

    /**
     * Register a new Artifact with `relevance_status = 'active'` and
     * return its generated id.
     *
     * @example
     * ```typescript
     * const artifactId = await db.artifact.cmd.create({
     *     title: 'design.md', description: 'system design',
     *     filepath: 'docs/design.md', reason: 'initial spec',
     *     provenanceId: 1, agentId: 1,
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateArtifactInput.parse(input);

        const [row] = await this.ctx.proc('sp_Artifact_Create', {
            p_title:         args.title,
            p_description:   args.description,
            p_filepath:      args.filepath,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
        });

        if (!row) throw new Error('sp_Artifact_Create returned no rows');

        return row.artifact_id;

    }

    /**
     * Overwrite an Artifact's mutable text columns. The proc excludes
     * `relevance_status` deliberately — change that via `setRelevance`
     * (or `softDelete` / `restore`) so the state-machine audit row is
     * written.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.update({
     *     artifactId: 1, title: 'design.md',
     *     description: 'system design', filepath: 'docs/design.md',
     *     reason: 'expanded scope section',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateArtifactInput.parse(input);

        await this.ctx.proc('sp_Artifact_Update', {
            p_artifact_id: args.artifactId,
            p_title:       args.title,
            p_description: args.description,
            p_filepath:    args.filepath,
            p_reason:      args.reason,
        });

    }

    /**
     * Drive a state-machine transition on the Artifact's `relevance_status`.
     * The proc validates the (current → new) edge against
     * `RelevanceStatus_Allowed` and writes an `Artifact_StateTransition`
     * row in the same transaction. Rejected if the edge is not allowed.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.setRelevance({
     *     artifactId: 1, newRelevanceStatus: 'superseded',
     *     agentId: 1, reason: 'replaced by design-v2.md',
     * });
     * ```
     */
    async setRelevance(input: unknown): Promise<void> {

        const args = SetRelevanceInput.parse(input);

        await this.ctx.proc('sp_Artifact_SetRelevance', {
            p_artifact_id:          args.artifactId,
            p_new_relevance_status: args.newRelevanceStatus,
            p_agent_id:             args.agentId,
            p_reason:               args.reason,
        });

    }

    /**
     * Soft-delete an Artifact by transitioning its `relevance_status` to
     * `'deleted'`. Forwards to `sp_Artifact_Delete`, which itself wraps
     * `sp_Artifact_SetRelevance(artifact_id, 'deleted', ...)`.
     *
     * Named `softDelete` (not `delete`) to match the project-wide convention
     * and avoid colliding with the JS `delete` keyword as a method name.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.softDelete({
     *     artifactId: 1, agentId: 1, reason: 'no longer relevant',
     * });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteArtifactInput.parse(input);

        await this.ctx.proc('sp_Artifact_Delete', {
            p_artifact_id: args.artifactId,
            p_agent_id:    args.agentId,
            p_reason:      args.reason,
        });

    }

    /**
     * Restore a previously soft-deleted Artifact by transitioning its
     * `relevance_status` back to `'active'`. Forwards to
     * `sp_Artifact_Restore`, which wraps
     * `sp_Artifact_SetRelevance(artifact_id, 'active', ...)`.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.restore({
     *     artifactId: 1, agentId: 1, reason: 'turned out to still be useful',
     * });
     * ```
     */
    async restore(input: unknown): Promise<void> {

        const args = RestoreArtifactInput.parse(input);

        await this.ctx.proc('sp_Artifact_Restore', {
            p_artifact_id: args.artifactId,
            p_agent_id:    args.agentId,
            p_reason:      args.reason,
        });

    }

    /**
     * Attach an Artifact to a Milestone via `Milestone_Artifact`.
     * Idempotent — re-attaching is a silent no-op at the SQL layer.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.attachMilestone({
     *     artifactId: 1, milestoneId: 1,
     * });
     * ```
     */
    async attachMilestone(input: unknown): Promise<void> {

        const args = AttachArtifactToMilestoneInput.parse(input);

        await this.ctx.proc('sp_Artifact_Attach_Milestone', {
            p_artifact_id:  args.artifactId,
            p_milestone_id: args.milestoneId,
        });

    }

    /**
     * Detach an Artifact from a Milestone. Idempotent — detaching a
     * non-existent join row is a silent no-op.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.detachMilestone({
     *     artifactId: 1, milestoneId: 1,
     * });
     * ```
     */
    async detachMilestone(input: unknown): Promise<void> {

        const args = DetachArtifactFromMilestoneInput.parse(input);

        await this.ctx.proc('sp_Artifact_Detach_Milestone', {
            p_artifact_id:  args.artifactId,
            p_milestone_id: args.milestoneId,
        });

    }

    /**
     * Attach an Artifact to a Task via `Task_Artifact`. Tasks are
     * identified by the composite key (`milestoneId`, `taskNo`).
     * Idempotent at the SQL layer.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.attachTask({
     *     artifactId: 1, milestoneId: 1, taskNo: 3,
     * });
     * ```
     */
    async attachTask(input: unknown): Promise<void> {

        const args = AttachArtifactToTaskInput.parse(input);

        await this.ctx.proc('sp_Artifact_Attach_Task', {
            p_artifact_id:  args.artifactId,
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
        });

    }

    /**
     * Detach an Artifact from a Task. Idempotent.
     *
     * @example
     * ```typescript
     * await db.artifact.cmd.detachTask({
     *     artifactId: 1, milestoneId: 1, taskNo: 3,
     * });
     * ```
     */
    async detachTask(input: unknown): Promise<void> {

        const args = DetachArtifactFromTaskInput.parse(input);

        await this.ctx.proc('sp_Artifact_Detach_Task', {
            p_artifact_id:  args.artifactId,
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
        });

    }

}
