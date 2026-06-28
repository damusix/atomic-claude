/**
 * DML for the Tag domain.
 *
 * 14 procs cover the inclusive-subtype lifecycle: 3 CRUD + Merge,
 * plus 5 idempotent attach/detach pairs (one per related entity
 * type), plus a bulk Attach_Memory variant that takes a TagAttachmentInput
 * TVP. Each method validates with Zod, then dispatches to the matching
 * proc; sentinel + duplicate-name guards live in SQL and propagate via
 * RAISERROR.
 *
 * @example
 * const tags = new TagCommands(ctx);
 * const { tag_id } = await tags.create({ name: 'design' });
 * await tags.attachMemory({ tagId: tag_id, memoryId: 42 });
 * await tags.bulkAttachMemory({
 *     pairs: [{ tagId: tag_id, memoryId: 43 }, { tagId: tag_id, memoryId: 44 }],
 * });
 */
import { tvp } from '@noormdev/sdk';

import { Repo } from '../core/repo';

import {
    AttachTagToArtifactInput,
    AttachTagToMemoryInput,
    AttachTagToMilestoneInput,
    AttachTagToProjectInput,
    AttachTagToTaskInput,
    BulkAttachMemoryInput,
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

export class TagCommands extends Repo {

    /** Insert a new Tag with a unique name; returns the IDENTITY id. */
    async create(input: unknown): Promise<{ tag_id: number }> {

        const parsed = CreateTagInput.parse(input);

        const rows = await this.ctx.proc('sp_Tag_Create', {
            name: parsed.name,
            description: parsed.description,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
        });

        const row = rows[0];

        if (!row) {

            throw new Error('sp_Tag_Create returned no rows.');
        }

        return row;

    }

    /** Update Tag metadata (name uniqueness re-validated by the DB). */
    async update(input: unknown): Promise<void> {

        const parsed = UpdateTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Update', {
            tag_id: parsed.tagId,
            name: parsed.name,
            description: parsed.description,
            reason: parsed.reason,
        });

    }

    /** Hard-delete a Tag; FK CASCADE wipes every *_Tag attachment. */
    async delete(input: unknown): Promise<void> {

        const parsed = DeleteTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Delete', {
            tag_id: parsed.tagId,
        });

    }

    /** Re-point all attachments from source to target tag, then drop source. */
    async merge(input: unknown): Promise<void> {

        const parsed = MergeTagInput.parse(input);

        await this.ctx.proc('sp_Tag_Merge', {
            source_tag_id: parsed.sourceTagId,
            target_tag_id: parsed.targetTagId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Idempotently attach a Tag to a Project. */
    async attachProject(input: unknown): Promise<void> {

        const parsed = AttachTagToProjectInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Project', {
            tag_id: parsed.tagId,
            project_id: parsed.projectId,
        });

    }

    /** Detach a Tag from a Project (no-op if not attached). */
    async detachProject(input: unknown): Promise<void> {

        const parsed = DetachTagFromProjectInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Project', {
            tag_id: parsed.tagId,
            project_id: parsed.projectId,
        });

    }

    /** Idempotently attach a Tag to a Memory. */
    async attachMemory(input: unknown): Promise<void> {

        const parsed = AttachTagToMemoryInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Memory', {
            tag_id: parsed.tagId,
            memory_id: parsed.memoryId,
        });

    }

    /** Detach a Tag from a Memory (no-op if not attached). */
    async detachMemory(input: unknown): Promise<void> {

        const parsed = DetachTagFromMemoryInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Memory', {
            tag_id: parsed.tagId,
            memory_id: parsed.memoryId,
        });

    }

    /** Idempotently attach a Tag to an Artifact. */
    async attachArtifact(input: unknown): Promise<void> {

        const parsed = AttachTagToArtifactInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Artifact', {
            tag_id: parsed.tagId,
            artifact_id: parsed.artifactId,
        });

    }

    /** Detach a Tag from an Artifact (no-op if not attached). */
    async detachArtifact(input: unknown): Promise<void> {

        const parsed = DetachTagFromArtifactInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Artifact', {
            tag_id: parsed.tagId,
            artifact_id: parsed.artifactId,
        });

    }

    /** Idempotently attach a Tag to a Milestone. */
    async attachMilestone(input: unknown): Promise<void> {

        const parsed = AttachTagToMilestoneInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Milestone', {
            tag_id: parsed.tagId,
            milestone_id: parsed.milestoneId,
        });

    }

    /** Detach a Tag from a Milestone (no-op if not attached). */
    async detachMilestone(input: unknown): Promise<void> {

        const parsed = DetachTagFromMilestoneInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Milestone', {
            tag_id: parsed.tagId,
            milestone_id: parsed.milestoneId,
        });

    }

    /** Idempotently attach a Tag to a Task (composite milestone+task_no). */
    async attachTask(input: unknown): Promise<void> {

        const parsed = AttachTagToTaskInput.parse(input);

        await this.ctx.proc('sp_Tag_Attach_Task', {
            tag_id: parsed.tagId,
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
        });

    }

    /** Detach a Tag from a Task (no-op if not attached). */
    async detachTask(input: unknown): Promise<void> {

        const parsed = DetachTagFromTaskInput.parse(input);

        await this.ctx.proc('sp_Tag_Detach_Task', {
            tag_id: parsed.tagId,
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
        });

    }

    /**
     * Set-based attach of many (tag_id, memory_id) pairs in one round trip.
     * Idempotent: rows whose pair already exists are skipped.
     */
    async bulkAttachMemory(input: unknown): Promise<void> {

        const { pairs } = BulkAttachMemoryInput.parse(input);

        await this.ctx.proc('sp_Tag_Bulk_Attach_Memory', {
            Pairs: tvp('TagAttachmentInput', pairs.map((p) => ({
                tag_id: p.tagId,
                entity_id: p.memoryId,
            }))),
        });

    }

}
