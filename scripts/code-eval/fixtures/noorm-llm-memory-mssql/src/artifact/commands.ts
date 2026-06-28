/**
 * DML for the Artifact domain.
 *
 * Each method validates input with a Zod schema, then dispatches to the
 * matching stored procedure. State-machine validation, sentinel guards,
 * and the StateTransition audit row all live in the procs — we let
 * RAISERROR propagate as a tedious error.
 *
 * @example
 * const artifacts = new ArtifactCommands(ctx);
 * const { artifact_id } = await artifacts.create({ title: 'design.png' });
 * await artifacts.attachMilestone({ artifactId: artifact_id, milestoneId: 1 });
 */
import { Repo } from '../core/repo';

import {
    AttachArtifactToMilestoneInput,
    AttachArtifactToTaskInput,
    CreateArtifactInput,
    DeleteArtifactInput,
    DetachArtifactFromMilestoneInput,
    DetachArtifactFromTaskInput,
    RestoreArtifactInput,
    SetArtifactRelevanceInput,
    UpdateArtifactInput,
} from './schema';

export class ArtifactCommands extends Repo {

    /** Insert a new Artifact (defaults to active relevance) and return its id. */
    async create(input: unknown): Promise<{ artifact_id: number }> {

        const parsed = CreateArtifactInput.parse(input);

        const rows = await this.ctx.proc('sp_Artifact_Create', {
            title: parsed.title,
            description: parsed.description,
            filepath: parsed.filepath,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Artifact_Create returned no rows.');

        return row;

    }

    /** Update Artifact metadata (title/description/filepath/reason). */
    async update(input: unknown) {

        const parsed = UpdateArtifactInput.parse(input);

        return this.ctx.proc('sp_Artifact_Update', {
            artifact_id: parsed.artifactId,
            title: parsed.title,
            description: parsed.description,
            filepath: parsed.filepath,
            reason: parsed.reason,
        });

    }

    /** Move relevance through the gated state machine and write an audit row. */
    async setRelevance(input: unknown) {

        const parsed = SetArtifactRelevanceInput.parse(input);

        return this.ctx.proc('sp_Artifact_SetRelevance', {
            artifact_id: parsed.artifactId,
            new_relevance_status: parsed.newRelevanceStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-delete via SetRelevance → 'deleted'. */
    async delete(input: unknown) {

        const parsed = DeleteArtifactInput.parse(input);

        return this.ctx.proc('sp_Artifact_Delete', {
            artifact_id: parsed.artifactId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-undelete via SetRelevance → 'active'. */
    async restore(input: unknown) {

        const parsed = RestoreArtifactInput.parse(input);

        return this.ctx.proc('sp_Artifact_Restore', {
            artifact_id: parsed.artifactId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Idempotent attach to a Milestone via Milestone_Artifact. */
    async attachMilestone(input: unknown) {

        const parsed = AttachArtifactToMilestoneInput.parse(input);

        return this.ctx.proc('sp_Artifact_Attach_Milestone', {
            artifact_id: parsed.artifactId,
            milestone_id: parsed.milestoneId,
        });

    }

    /** Idempotent detach from a Milestone — silent on missing row. */
    async detachMilestone(input: unknown) {

        const parsed = DetachArtifactFromMilestoneInput.parse(input);

        return this.ctx.proc('sp_Artifact_Detach_Milestone', {
            artifact_id: parsed.artifactId,
            milestone_id: parsed.milestoneId,
        });

    }

    /** Idempotent attach to a Task via Task_Artifact. */
    async attachTask(input: unknown) {

        const parsed = AttachArtifactToTaskInput.parse(input);

        return this.ctx.proc('sp_Artifact_Attach_Task', {
            artifact_id: parsed.artifactId,
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
        });

    }

    /** Idempotent detach from a Task — silent on missing row. */
    async detachTask(input: unknown) {

        const parsed = DetachArtifactFromTaskInput.parse(input);

        return this.ctx.proc('sp_Artifact_Detach_Task', {
            artifact_id: parsed.artifactId,
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
        });

    }

}
