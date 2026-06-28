import { Repo } from '../core/repo';

import {
    AttachProjectInput,
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

/**
 * Mutation surface for the `Memory` domain.
 *
 * One method per `sp_Memory_*` stored procedure, in the same order they
 * appear in the schema artifact. Each method:
 *
 *   1. Parses untyped input through the matching Zod schema.
 *   2. Maps camelCase fields onto the proc's snake_case args.
 *   3. Awaits the proc and unwraps the return value where useful
 *      (`create` returns a bare `number` rather than `{ memory_id }`).
 *
 * State-machine guards (`fn_IsRelevanceTransitionAllowed`),
 * self-reference guards (Relate / Consolidate), and idempotency
 * (Attach / Detach / Relate / Unrelate's `ON CONFLICT DO NOTHING`)
 * all live in SQL — this layer just forwards the call.
 *
 * @example
 * ```typescript
 * const memoryId = await db.memory.cmd.create({
 *     content: 'kysely returns Generated<T> for default columns',
 *     domain: 'backend',
 *     category: 'fact',
 *     reason: 'observed while authoring memory domain',
 *     provenanceId: 0,
 *     agentId: 7,
 *     wasObserved: true,
 *     wasEvidenced: true,
 * });
 *
 * await db.memory.cmd.touch({ memoryId, agentId: 7 });
 * await db.memory.cmd.softDelete({ memoryId, agentId: 7, reason: 'no longer accurate' });
 * ```
 */
export class MemoryCommands extends Repo {

    /**
     * Record a new memory and return its id.
     *
     * @example
     * ```typescript
     * const memoryId = await db.memory.cmd.create({
     *     content: 'pg defaults vs kysely Generated<T> are aligned',
     *     domain: 'backend',
     *     category: 'fact',
     *     reason: 'observed during schema authoring',
     *     provenanceId: 0,
     *     agentId: 7,
     *     wasObserved: true,
     *     wasEvidenced: true,
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateMemoryInput.parse(input);

        const [row] = await this.ctx.proc('sp_Memory_Create', {
            p_content:           args.content,
            p_domain:            args.domain,
            p_category:          args.category,
            p_reason:            args.reason,
            p_provenance_id:     args.provenanceId,
            p_agent_id:          args.agentId,
            p_was_inferred:      args.wasInferred,
            p_was_observed:      args.wasObserved,
            p_was_evidenced:     args.wasEvidenced,
            p_was_user_provided: args.wasUserProvided,
        });

        if (!row) throw new Error('sp_Memory_Create returned no rows');

        return row.memory_id;

    }

    /**
     * Edit a memory's content, classification, and confidence flags.
     * Status changes go through `setRelevance` so this method never
     * touches `relevance_status`.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.update({
     *     memoryId: 42,
     *     content: 'updated wording',
     *     domain: 'backend',
     *     category: 'fact',
     *     reason: 'clarified after review',
     *     wasInferred: false,
     *     wasObserved: true,
     *     wasEvidenced: true,
     *     wasUserProvided: false,
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Update', {
            p_memory_id:         args.memoryId,
            p_content:           args.content,
            p_domain:            args.domain,
            p_category:          args.category,
            p_reason:            args.reason,
            p_was_inferred:      args.wasInferred,
            p_was_observed:      args.wasObserved,
            p_was_evidenced:     args.wasEvidenced,
            p_was_user_provided: args.wasUserProvided,
        });

    }

    /**
     * Transition a memory's `relevance_status`. Validated against
     * `RelevanceStatus_Allowed` in SQL — invalid `(from, to)` pairs
     * are rejected before any update. Every successful transition
     * also writes a `StateTransition` audit row.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.setRelevance({
     *     memoryId: 42,
     *     newRelevanceStatus: 'superseded',
     *     agentId: 7,
     *     reason: 'replaced by memory 99',
     * });
     * ```
     */
    async setRelevance(input: unknown): Promise<void> {

        const args = SetRelevanceInput.parse(input);

        await this.ctx.proc('sp_Memory_SetRelevance', {
            p_memory_id:            args.memoryId,
            p_new_relevance_status: args.newRelevanceStatus,
            p_agent_id:             args.agentId,
            p_reason:               args.reason,
        });

    }

    /**
     * Soft-delete a memory by transitioning to `relevance_status =
     * 'deleted'`. Recoverable via `restore` until `sp_Cleanup`
     * hard-deletes past the TTL.
     *
     * Named `softDelete` (not `delete`) to match the project-wide
     * convention and avoid colliding with the JS `delete` keyword.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.softDelete({
     *     memoryId: 42,
     *     agentId: 7,
     *     reason: 'no longer accurate',
     * });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Delete', {
            p_memory_id: args.memoryId,
            p_agent_id:  args.agentId,
            p_reason:    args.reason,
        });

    }

    /**
     * Restore a soft-deleted memory back to `relevance_status =
     * 'active'`. Useful before `sp_Cleanup` expires the row.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.restore({
     *     memoryId: 42,
     *     agentId: 7,
     *     reason: 'still relevant after second look',
     * });
     * ```
     */
    async restore(input: unknown): Promise<void> {

        const args = RestoreMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Restore', {
            p_memory_id: args.memoryId,
            p_agent_id:  args.agentId,
            p_reason:    args.reason,
        });

    }

    /**
     * Bump `last_accessed_at` and `access_count` without modifying
     * `updated_at`. Read-side signal — applications must call this
     * on every retrieval so `fn_MemoryRank` has fresh recency data.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.touch({ memoryId: 42, agentId: 7 });
     * ```
     */
    async touch(input: unknown): Promise<void> {

        const args = TouchMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Touch', {
            p_memory_id: args.memoryId,
            p_agent_id:  args.agentId,
        });

    }

    /**
     * Record a directed edge in `Related_Memory` with a forward verb.
     * The inverse direction surfaces automatically through
     * `vw_Related_Memory` (substituting `verb_backward`). Self-edges
     * are rejected by the SQL guard.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.relate({
     *     memoryId: 99,
     *     relatedMemoryId: 42,
     *     relationVerb: 'supersedes',
     *     reason: 'rewritten with more precise wording',
     * });
     * ```
     */
    async relate(input: unknown): Promise<void> {

        const args = RelateMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Relate', {
            p_memory_id:         args.memoryId,
            p_related_memory_id: args.relatedMemoryId,
            p_relation_verb:     args.relationVerb,
            p_reason:            args.reason,
        });

    }

    /**
     * Drop the stored direction in `Related_Memory`. Idempotent —
     * removing an edge that doesn't exist is a silent no-op.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.unrelate({ memoryId: 99, relatedMemoryId: 42 });
     * ```
     */
    async unrelate(input: unknown): Promise<void> {

        const args = UnrelateMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Unrelate', {
            p_memory_id:         args.memoryId,
            p_related_memory_id: args.relatedMemoryId,
        });

    }

    /**
     * Merge a duplicate memory into a canonical one. The proc links
     * them via a `'supersedes'` relation, re-points `Memory_Tag` and
     * `Project_Memory` rows from duplicate to canonical, and
     * transitions the duplicate to `relevance_status = 'superseded'`.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.consolidate({
     *     canonicalMemoryId: 42,
     *     duplicateMemoryId: 99,
     *     agentId: 7,
     *     reason: 'same fact in two memories — kept the older wording',
     * });
     * ```
     */
    async consolidate(input: unknown): Promise<void> {

        const args = ConsolidateMemoryInput.parse(input);

        await this.ctx.proc('sp_Memory_Consolidate', {
            p_canonical_memory_id: args.canonicalMemoryId,
            p_duplicate_memory_id: args.duplicateMemoryId,
            p_agent_id:            args.agentId,
            p_reason:              args.reason,
        });

    }

    /**
     * Associate a memory with a project (`Project_Memory`). Idempotent
     * — duplicate attaches are silent no-ops at the SQL layer.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.attachProject({ memoryId: 42, projectId: 1 });
     * ```
     */
    async attachProject(input: unknown): Promise<void> {

        const args = AttachProjectInput.parse(input);

        await this.ctx.proc('sp_Memory_Attach_Project', {
            p_memory_id:  args.memoryId,
            p_project_id: args.projectId,
        });

    }

    /**
     * Remove a memory ↔ project association (`Project_Memory`).
     * Idempotent — detaching a row that doesn't exist is a silent
     * no-op.
     *
     * @example
     * ```typescript
     * await db.memory.cmd.detachProject({ memoryId: 42, projectId: 1 });
     * ```
     */
    async detachProject(input: unknown): Promise<void> {

        const args = DetachProjectInput.parse(input);

        await this.ctx.proc('sp_Memory_Detach_Project', {
            p_memory_id:  args.memoryId,
            p_project_id: args.projectId,
        });

    }

}
