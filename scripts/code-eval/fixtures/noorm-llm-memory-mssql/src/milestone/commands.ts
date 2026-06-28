/**
 * DML for the Milestone domain.
 *
 * Each method validates input with a Zod schema, then dispatches to the
 * matching stored procedure. Two state-machine axes (tracking + relevance)
 * are exposed as separate methods that mirror the underlying procs.
 *
 * `close` wraps sp_Milestone_Close, which fans out to SetTracking ('done'),
 * SetRelevance ('superseded'), and per-task SetTracking ('abandoned') for
 * every open child Task — all inside a single proc-side transaction. We
 * deliberately do NOT replicate that orchestration in TypeScript.
 *
 * @example
 * const milestones = new MilestoneCommands(ctx);
 * const { milestone_id } = await milestones.create({ title: 'v2 launch' });
 * await milestones.close({ milestoneId: milestone_id, agentId: 1 });
 */
import { Repo } from '../core/repo';

import {
    AttachMilestoneToProjectInput,
    CloseMilestoneInput,
    CreateMilestoneInput,
    DeleteMilestoneInput,
    DetachMilestoneFromProjectInput,
    RestoreMilestoneInput,
    SetMilestoneRelevanceInput,
    SetMilestoneTrackingInput,
    UpdateMilestoneInput,
} from './schema';

export class MilestoneCommands extends Repo {

    /** Insert a new Milestone (defaults to not-started/active) and return its id. */
    async create(input: unknown): Promise<{ milestone_id: number }> {

        const parsed = CreateMilestoneInput.parse(input);

        const rows = await this.ctx.proc('sp_Milestone_Create', {
            title: parsed.title,
            content: parsed.content,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Milestone_Create returned no rows.');

        return row;

    }

    /** Update Milestone metadata (title/content/reason). Status changes use the dedicated methods. */
    async update(input: unknown) {

        const parsed = UpdateMilestoneInput.parse(input);

        return this.ctx.proc('sp_Milestone_Update', {
            milestone_id: parsed.milestoneId,
            title: parsed.title,
            content: parsed.content,
            reason: parsed.reason,
        });

    }

    /** Move tracking_status through the gated state machine and write an audit row. */
    async setTracking(input: unknown) {

        const parsed = SetMilestoneTrackingInput.parse(input);

        return this.ctx.proc('sp_Milestone_SetTracking', {
            milestone_id: parsed.milestoneId,
            new_tracking_status: parsed.newTrackingStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Move relevance_status through the gated state machine and write an audit row. */
    async setRelevance(input: unknown) {

        const parsed = SetMilestoneRelevanceInput.parse(input);

        return this.ctx.proc('sp_Milestone_SetRelevance', {
            milestone_id: parsed.milestoneId,
            new_relevance_status: parsed.newRelevanceStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-delete: cascade-soft-delete attached Notes, then SetRelevance → 'deleted'. */
    async delete(input: unknown) {

        const parsed = DeleteMilestoneInput.parse(input);

        return this.ctx.proc('sp_Milestone_Delete', {
            milestone_id: parsed.milestoneId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-undelete via SetRelevance → 'active'. */
    async restore(input: unknown) {

        const parsed = RestoreMilestoneInput.parse(input);

        return this.ctx.proc('sp_Milestone_Restore', {
            milestone_id: parsed.milestoneId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /**
     * Close the Milestone — proc-side orchestration sets tracking to
     * 'done', relevance to 'superseded', and abandons every open child
     * Task. Errors from any inner transition roll back the whole batch.
     */
    async close(input: unknown) {

        const parsed = CloseMilestoneInput.parse(input);

        return this.ctx.proc('sp_Milestone_Close', {
            milestone_id: parsed.milestoneId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Idempotent attach to a Project via Project_Milestone. */
    async attachProject(input: unknown) {

        const parsed = AttachMilestoneToProjectInput.parse(input);

        return this.ctx.proc('sp_Milestone_Attach_Project', {
            milestone_id: parsed.milestoneId,
            project_id: parsed.projectId,
        });

    }

    /** Detach from a Project — silent on missing row. */
    async detachProject(input: unknown) {

        const parsed = DetachMilestoneFromProjectInput.parse(input);

        return this.ctx.proc('sp_Milestone_Detach_Project', {
            milestone_id: parsed.milestoneId,
            project_id: parsed.projectId,
        });

    }

}
