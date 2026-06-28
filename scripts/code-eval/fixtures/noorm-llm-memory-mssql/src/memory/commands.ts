/**
 * DML for the Memory domain.
 *
 * Each method validates input with a Zod schema, then dispatches to the
 * matching stored procedure. Reference-table guards (domain/category/
 * relation_verb), state-machine gating, and self-relation checks all
 * live in the proc — RAISERROR propagates as a tedious error.
 *
 * @example
 * const memories = new MemoryCommands(ctx);
 * const { memory_id } = await memories.create({
 *     content: 'User prefers dark mode',
 *     domain: 'user-preference',
 *     category: 'ui',
 * });
 */
import { tvp } from '@noormdev/sdk';

import { Repo } from '../core/repo';

import {
    AttachProjectInput,
    BulkTouchInput,
    ConsolidateMemoryInput,
    CreateMemoryInput,
    DeleteMemoryInput,
    DetachProjectInput,
    RelateMemoryInput,
    RestoreMemoryInput,
    SetRelevanceInput,
    TouchMemoryInput,
    UnrelateMemoryInput,
    UpdateMemoryInput,
} from './schema';

export class MemoryCommands extends Repo {

    /** Insert a new Memory (relevance_status='active') and return its IDENTITY id. */
    async create(input: unknown): Promise<{ memory_id: number }> {

        const parsed = CreateMemoryInput.parse(input);

        const rows = await this.ctx.proc('sp_Memory_Create', {
            content: parsed.content,
            domain: parsed.domain,
            category: parsed.category,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
            was_inferred: parsed.wasInferred,
            was_observed: parsed.wasObserved,
            was_evidenced: parsed.wasEvidenced,
            was_user_provided: parsed.wasUserProvided,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Memory_Create returned no rows.');

        return row;

    }

    /** Modify content/domain/category/booleans (relevance_status is unchanged). */
    async update(input: unknown) {

        const parsed = UpdateMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Update', {
            memory_id: parsed.memoryId,
            content: parsed.content,
            domain: parsed.domain,
            category: parsed.category,
            reason: parsed.reason,
            was_inferred: parsed.wasInferred,
            was_observed: parsed.wasObserved,
            was_evidenced: parsed.wasEvidenced,
            was_user_provided: parsed.wasUserProvided,
        });

    }

    /** Gated relevance transition; also writes a Memory_StateTransition audit row. */
    async setRelevance(input: unknown) {

        const parsed = SetRelevanceInput.parse(input);

        return this.ctx.proc('sp_Memory_SetRelevance', {
            memory_id: parsed.memoryId,
            new_relevance_status: parsed.newRelevanceStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-delete via SetRelevance to 'deleted'. Hard delete is sp_Cleanup's job. */
    async delete(input: unknown) {

        const parsed = DeleteMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Delete', {
            memory_id: parsed.memoryId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Restore via SetRelevance to 'active'. Allowed only from valid prior states. */
    async restore(input: unknown) {

        const parsed = RestoreMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Restore', {
            memory_id: parsed.memoryId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Bump last_accessed_at + access_count (read-side signal — no updated_at). */
    async touch(input: unknown) {

        const parsed = TouchMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Touch', {
            memory_id: parsed.memoryId,
            agent_id: parsed.agentId,
        });

    }

    /** Idempotent insert into Related_Memory; verb validated against MemoryRelationVerb. */
    async relate(input: unknown) {

        const parsed = RelateMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Relate', {
            memory_id: parsed.memoryId,
            related_memory_id: parsed.relatedMemoryId,
            relation_verb: parsed.relationVerb,
            reason: parsed.reason,
        });

    }

    /** Silent delete from Related_Memory — detach is idempotent. */
    async unrelate(input: unknown) {

        const parsed = UnrelateMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Unrelate', {
            memory_id: parsed.memoryId,
            related_memory_id: parsed.relatedMemoryId,
        });

    }

    /** Merge duplicate into canonical: re-point tags + projects, then mark superseded. */
    async consolidate(input: unknown) {

        const parsed = ConsolidateMemoryInput.parse(input);

        return this.ctx.proc('sp_Memory_Consolidate', {
            canonical_memory_id: parsed.canonicalMemoryId,
            duplicate_memory_id: parsed.duplicateMemoryId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Idempotent insert into Project_Memory. */
    async attachProject(input: unknown) {

        const parsed = AttachProjectInput.parse(input);

        return this.ctx.proc('sp_Memory_Attach_Project', {
            memory_id: parsed.memoryId,
            project_id: parsed.projectId,
        });

    }

    /** Silent delete from Project_Memory. */
    async detachProject(input: unknown) {

        const parsed = DetachProjectInput.parse(input);

        return this.ctx.proc('sp_Memory_Detach_Project', {
            memory_id: parsed.memoryId,
            project_id: parsed.projectId,
        });

    }

    /**
     * Bulk variant of touch — single set-based UPDATE keyed by a MemoryIdSet
     * TVP. Use when batching read-side signals from a retrieval pipeline.
     */
    async bulkTouch(input: unknown) {

        const parsed = BulkTouchInput.parse(input);

        return this.ctx.proc('sp_Memory_Bulk_Touch', {
            MemoryIds: tvp('MemoryIdSet', parsed.memoryIds.map((memory_id) => ({
                memory_id,
            }))),
        });

    }

}
