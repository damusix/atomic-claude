import { z } from 'zod';

/**
 * Input shape for `MemoryCommands.create`.
 *
 * Mirrors `sp_Memory_Create`'s parameter list. The four `was_*` booleans
 * default to `false` so callers can record a memory with just content
 * and classification — opting into a confidence claim is an explicit
 * decision, not an accident.
 *
 * `provenanceId` and `agentId` are non-negative because both reference
 * sentinel id `0` ("none") for un-attributed records.
 *
 * @example
 * ```typescript
 * const args = CreateMemoryInput.parse({
 *     content: 'Kysely returns Generated<T> for default columns',
 *     domain: 'backend',
 *     category: 'fact',
 *     reason: 'observed while authoring memory domain',
 *     provenanceId: 0,
 *     agentId: 7,
 *     wasObserved: true,
 *     wasEvidenced: true,
 * });
 * ```
 */
export const CreateMemoryInput = z.object({
    content:         z.string().min(1),
    domain:          z.string().min(1),
    category:        z.string().min(1),
    reason:          z.string().default(''),
    provenanceId:    z.number().int().nonnegative(),
    agentId:         z.number().int().nonnegative(),
    wasInferred:     z.boolean().default(false),
    wasObserved:     z.boolean().default(false),
    wasEvidenced:    z.boolean().default(false),
    wasUserProvided: z.boolean().default(false),
});
export type CreateMemoryInput = z.infer<typeof CreateMemoryInput>;

/**
 * Input shape for `MemoryCommands.update`.
 *
 * Mirrors `sp_Memory_Update` — content / classification / confidence
 * booleans only. `relevance_status` is intentionally absent: status
 * changes go through `setRelevance` so every transition produces a
 * matching `StateTransition` audit row.
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
export const UpdateMemoryInput = z.object({
    memoryId:        z.number().int().positive(),
    content:         z.string().min(1),
    domain:          z.string().min(1),
    category:        z.string().min(1),
    reason:          z.string(),
    wasInferred:     z.boolean(),
    wasObserved:     z.boolean(),
    wasEvidenced:    z.boolean(),
    wasUserProvided: z.boolean(),
});
export type UpdateMemoryInput = z.infer<typeof UpdateMemoryInput>;

/**
 * Input shape for `MemoryCommands.setRelevance`.
 *
 * `newRelevanceStatus` is constrained to the five values defined by the
 * `RelevanceStatus` reference table — caller-side guard so a typo
 * surfaces as a Zod error before it ever reaches the SQL state machine
 * (`fn_IsRelevanceTransitionAllowed`).
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
export const SetRelevanceInput = z.object({
    memoryId:           z.number().int().positive(),
    newRelevanceStatus: z.enum(['active', 'needs-review', 'superseded', 'irrelevant', 'deleted']),
    agentId:            z.number().int().nonnegative(),
    reason:             z.string(),
});
export type SetRelevanceInput = z.infer<typeof SetRelevanceInput>;

/**
 * Input shape for `MemoryCommands.softDelete`.
 *
 * Wraps `sp_Memory_Delete`, which transitions the row to
 * `relevance_status = 'deleted'` (recoverable until `sp_Cleanup`
 * hard-deletes past the TTL).
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
export const DeleteMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId:  z.number().int().nonnegative(),
    reason:   z.string(),
});
export type DeleteMemoryInput = z.infer<typeof DeleteMemoryInput>;

/**
 * Input shape for `MemoryCommands.restore`.
 *
 * Wraps `sp_Memory_Restore` — transitions a soft-deleted memory back
 * to `relevance_status = 'active'`. Used to recover a row before
 * `sp_Cleanup` expires it.
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
export const RestoreMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId:  z.number().int().nonnegative(),
    reason:   z.string(),
});
export type RestoreMemoryInput = z.infer<typeof RestoreMemoryInput>;

/**
 * Input shape for `MemoryCommands.touch`.
 *
 * `sp_Memory_Touch` is a read-side signal — bumps `last_accessed_at`
 * and `access_count` without touching `updated_at`. The application
 * is expected to call it on every memory retrieval so `fn_MemoryRank`
 * has fresh recency data to decay against.
 *
 * @example
 * ```typescript
 * await db.memory.cmd.touch({ memoryId: 42, agentId: 7 });
 * ```
 */
export const TouchMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId:  z.number().int().nonnegative(),
});
export type TouchMemoryInput = z.infer<typeof TouchMemoryInput>;

/**
 * Input shape for `MemoryCommands.relate`.
 *
 * Records a directed `Related_Memory` edge with a forward verb. The
 * inverse direction is exposed automatically via `vw_Related_Memory`
 * (substituting `verb_backward`). Self-references are rejected by the
 * SQL guard (`memory_id <> related_memory_id`).
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
export const RelateMemoryInput = z.object({
    memoryId:        z.number().int().positive(),
    relatedMemoryId: z.number().int().positive(),
    relationVerb:    z.string().min(1),
    reason:          z.string().default(''),
});
export type RelateMemoryInput = z.infer<typeof RelateMemoryInput>;

/**
 * Input shape for `MemoryCommands.unrelate`.
 *
 * Deletes the single stored direction in `Related_Memory`; the inverse
 * row in `vw_Related_Memory` disappears with it. Idempotent — no-ops
 * silently when no edge exists.
 *
 * @example
 * ```typescript
 * await db.memory.cmd.unrelate({ memoryId: 99, relatedMemoryId: 42 });
 * ```
 */
export const UnrelateMemoryInput = z.object({
    memoryId:        z.number().int().positive(),
    relatedMemoryId: z.number().int().positive(),
});
export type UnrelateMemoryInput = z.infer<typeof UnrelateMemoryInput>;

/**
 * Input shape for `MemoryCommands.consolidate`.
 *
 * Wraps `sp_Memory_Consolidate`: links the duplicate to the canonical
 * via a `'supersedes'` relation, re-points `Memory_Tag` and
 * `Project_Memory` rows from duplicate to canonical, then transitions
 * the duplicate to `relevance_status = 'superseded'`. Self-merge is
 * rejected at the SQL layer.
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
export const ConsolidateMemoryInput = z.object({
    canonicalMemoryId: z.number().int().positive(),
    duplicateMemoryId: z.number().int().positive(),
    agentId:           z.number().int().nonnegative(),
    reason:            z.string(),
});
export type ConsolidateMemoryInput = z.infer<typeof ConsolidateMemoryInput>;

/**
 * Input shape for `MemoryCommands.attachProject`.
 *
 * Inserts into `Project_Memory`. Idempotent — a duplicate attach is a
 * silent no-op at the SQL level.
 *
 * @example
 * ```typescript
 * await db.memory.cmd.attachProject({ memoryId: 42, projectId: 1 });
 * ```
 */
export const AttachProjectInput = z.object({
    memoryId:  z.number().int().positive(),
    projectId: z.number().int().nonnegative(),
});
export type AttachProjectInput = z.infer<typeof AttachProjectInput>;

/**
 * Input shape for `MemoryCommands.detachProject`.
 *
 * Deletes from `Project_Memory`. Idempotent — detaching a row that
 * doesn't exist is a silent no-op.
 *
 * @example
 * ```typescript
 * await db.memory.cmd.detachProject({ memoryId: 42, projectId: 1 });
 * ```
 */
export const DetachProjectInput = z.object({
    memoryId:  z.number().int().positive(),
    projectId: z.number().int().nonnegative(),
});
export type DetachProjectInput = z.infer<typeof DetachProjectInput>;

/**
 * Options accepted by `MemoryQueries.listActive`, `listDeleted`, and
 * `listByAgent`.
 *
 * Defaults match the project-wide convention: 50-row pages capped at
 * 500 to keep accidental "list everything" calls from sweeping the
 * whole table. `domain` and `category` are optional filters applied
 * with equality.
 *
 * @example
 * ```typescript
 * const opts = ListMemoryOpts.parse({ domain: 'backend', limit: 25 });
 * ```
 */
export const ListMemoryOpts = z.object({
    domain:   z.string().optional(),
    category: z.string().optional(),
    limit:    z.number().int().positive().max(500).default(50),
    offset:   z.number().int().nonnegative().default(0),
});
export type ListMemoryOpts = z.infer<typeof ListMemoryOpts>;

/**
 * Options accepted by `MemoryQueries.search`.
 *
 * Read-side concern only — there is no SQL search proc. `contentLike`
 * is a Kysely `LIKE` pattern (callers add `%` wildcards), `agentId`
 * filters by author. Both filters are optional; when neither is
 * supplied, `search` paginates the full active-memory view.
 *
 * @example
 * ```typescript
 * const matches = await db.memory.q.search({
 *     contentLike: '%kysely%',
 *     limit: 25,
 * });
 * ```
 */
export const SearchMemoryOpts = z.object({
    contentLike: z.string().optional(),
    agentId:     z.number().int().nonnegative().optional(),
    limit:       z.number().int().positive().max(500).default(50),
    offset:      z.number().int().nonnegative().default(0),
});
export type SearchMemoryOpts = z.infer<typeof SearchMemoryOpts>;
