import { Repo } from '../core/repo';

import {
    AttachTagToArtifactInput,
    AttachTagToMemoryInput,
    AttachTagToMilestoneInput,
    AttachTagToProjectInput,
    AttachTagToTaskInput,
    CreateTagInput,
    DeleteTagInput,
    DetachTagFromArtifactInput,
    DetachTagFromMemoryInput,
    DetachTagFromMilestoneInput,
    DetachTagFromProjectInput,
    DetachTagFromTaskInput,
    MergeTagInput,
    UpdateTagInput,
} from './schema';

/**
 * Mutation surface for the `Tag` domain.
 *
 * One method per `sp_Tag_*` proc — fourteen in total: Create, Update,
 * Remove, Merge, plus five Attach / five Detach pairs (Project, Memory,
 * Artifact, Milestone, Task). Each method:
 *
 *   1. Parses untyped input through the matching Zod schema.
 *   2. Maps camelCase fields onto the proc's snake_case args.
 *   3. Awaits the proc and unwraps the `{ tag_id }` shape on `create`.
 *
 * `remove` (rather than `softDelete`) is used for Delete because Tags have
 * no `relevance_status` — the proc is a true hard delete and the join
 * rows are swept by `ON DELETE CASCADE`. Attach / Detach calls are
 * idempotent at the SQL layer, so the wrappers don't add retry logic.
 *
 * @example
 * ```typescript
 * const tagId = await db.tag.cmd.create({
 *     name: 'backend-perf', provenanceId: 1, agentId: 1,
 * });
 * await db.tag.cmd.attachMemory({ tagId, memoryId: 42 });
 * await db.tag.cmd.merge({ sourceTagId: 12, targetTagId: tagId, agentId: 1, reason: 'dedup' });
 * ```
 */
export class TagCommands extends Repo {

    /**
     * Create a new Tag and return its generated id.
     *
     * The SQL proc rejects duplicate `name` values (the column is `UNIQUE`),
     * so callers can use the resulting error to drive an "already exists,
     * fetch existing" fallback.
     *
     * @example
     * ```typescript
     * const tagId = await db.tag.cmd.create({
     *     name: 'backend-perf',
     *     description: 'performance work in the backend',
     *     reason: 'introducing taxonomy for incident review',
     *     provenanceId: 1,
     *     agentId: 1,
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateTagInput.parse(input);

        const [row] = await this.ctx.proc('sp_Tag_Create', {
            p_name:          args.name,
            p_description:   args.description,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
        });

        if (!row) throw new Error('sp_Tag_Create returned no rows');

        return row.tag_id;

    }

    /**
     * Overwrite a Tag's mutable text columns. `color` is not editable through
     * this method — it lives outside the proc surface and is set via UI.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.update({
     *     tagId: 7, name: 'backend-perf',
     *     description: 'merged taxonomy',
     *     reason: 'consolidating duplicate tag',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Update', {
            p_tag_id:      args.tagId,
            p_name:        args.name,
            p_description: args.description,
            p_reason:      args.reason,
        });

    }

    /**
     * Hard-delete a Tag. Tags carry no `relevance_status`, so this is the
     * only delete path — there is no soft-delete equivalent. Cascade FKs on
     * the five `*_Tag` join tables remove every attachment atomically.
     *
     * Named `remove` (not `softDelete`) so the call site reads as the
     * destructive operation it is.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.remove({ tagId: 7 });
     * ```
     */
    async remove(input: unknown): Promise<void> {

        const args = DeleteTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Delete', {
            p_tag_id: args.tagId,
        });

    }

    /**
     * Attach this Tag to a Project. Idempotent — re-attaching is a no-op
     * (the proc uses `ON CONFLICT DO NOTHING`).
     *
     * @example
     * ```typescript
     * await db.tag.cmd.attachProject({ tagId: 7, projectId: 1 });
     * ```
     */
    async attachProject(input: unknown): Promise<void> {

        const args = AttachTagToProjectInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Project', {
            p_tag_id:     args.tagId,
            p_project_id: args.projectId,
        });

    }

    /**
     * Detach this Tag from a Project. Idempotent — detaching an absent
     * attachment is a no-op (DELETE matches zero rows).
     *
     * @example
     * ```typescript
     * await db.tag.cmd.detachProject({ tagId: 7, projectId: 1 });
     * ```
     */
    async detachProject(input: unknown): Promise<void> {

        const args = DetachTagFromProjectInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Project', {
            p_tag_id:     args.tagId,
            p_project_id: args.projectId,
        });

    }

    /**
     * Attach this Tag to a Memory. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.attachMemory({ tagId: 7, memoryId: 42 });
     * ```
     */
    async attachMemory(input: unknown): Promise<void> {

        const args = AttachTagToMemoryInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Memory', {
            p_tag_id:    args.tagId,
            p_memory_id: args.memoryId,
        });

    }

    /**
     * Detach this Tag from a Memory. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.detachMemory({ tagId: 7, memoryId: 42 });
     * ```
     */
    async detachMemory(input: unknown): Promise<void> {

        const args = DetachTagFromMemoryInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Memory', {
            p_tag_id:    args.tagId,
            p_memory_id: args.memoryId,
        });

    }

    /**
     * Attach this Tag to an Artifact. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.attachArtifact({ tagId: 7, artifactId: 99 });
     * ```
     */
    async attachArtifact(input: unknown): Promise<void> {

        const args = AttachTagToArtifactInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Artifact', {
            p_tag_id:      args.tagId,
            p_artifact_id: args.artifactId,
        });

    }

    /**
     * Detach this Tag from an Artifact. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.detachArtifact({ tagId: 7, artifactId: 99 });
     * ```
     */
    async detachArtifact(input: unknown): Promise<void> {

        const args = DetachTagFromArtifactInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Artifact', {
            p_tag_id:      args.tagId,
            p_artifact_id: args.artifactId,
        });

    }

    /**
     * Attach this Tag to a Milestone. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.attachMilestone({ tagId: 7, milestoneId: 3 });
     * ```
     */
    async attachMilestone(input: unknown): Promise<void> {

        const args = AttachTagToMilestoneInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Milestone', {
            p_tag_id:       args.tagId,
            p_milestone_id: args.milestoneId,
        });

    }

    /**
     * Detach this Tag from a Milestone. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.detachMilestone({ tagId: 7, milestoneId: 3 });
     * ```
     */
    async detachMilestone(input: unknown): Promise<void> {

        const args = DetachTagFromMilestoneInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Milestone', {
            p_tag_id:       args.tagId,
            p_milestone_id: args.milestoneId,
        });

    }

    /**
     * Attach this Tag to a Task. Tasks have a composite PK, so both
     * `milestoneId` and `taskNo` are required. Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.attachTask({ tagId: 7, milestoneId: 3, taskNo: 12 });
     * ```
     */
    async attachTask(input: unknown): Promise<void> {

        const args = AttachTagToTaskInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Task', {
            p_tag_id:       args.tagId,
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
        });

    }

    /**
     * Detach this Tag from a Task (composite PK). Idempotent.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.detachTask({ tagId: 7, milestoneId: 3, taskNo: 12 });
     * ```
     */
    async detachTask(input: unknown): Promise<void> {

        const args = DetachTagFromTaskInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Task', {
            p_tag_id:       args.tagId,
            p_milestone_id: args.milestoneId,
            p_task_no:      args.taskNo,
        });

    }

    /**
     * Merge `sourceTagId` into `targetTagId`.
     *
     * Re-points every `*_Tag` row that references the source onto the
     * target (skipping rows that would duplicate an existing target
     * attachment), then hard-deletes the source. The whole operation runs
     * inside a single transaction in SQL, so observers either see the
     * pre-merge state or the post-merge state — never an in-flight blend.
     *
     * Use case: an LLM realizes `backend-perf` and `backend-performance`
     * are the same concept and wants to consolidate without losing any
     * attachments.
     *
     * @example
     * ```typescript
     * await db.tag.cmd.merge({
     *     sourceTagId: 12, targetTagId: 7, agentId: 1,
     *     reason: 'duplicate taxonomy — same concept, different spelling',
     * });
     * ```
     */
    async merge(input: unknown): Promise<void> {

        const args = MergeTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Merge', {
            p_source_tag_id: args.sourceTagId,
            p_target_tag_id: args.targetTagId,
            p_agent_id:      args.agentId,
            p_reason:        args.reason,
        });

    }

}
