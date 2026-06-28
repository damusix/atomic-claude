import { z } from 'zod';

/**
 * Input for `ArtifactCommands.create`.
 *
 * `title` is the only field that must be supplied; every other VARCHAR
 * mirrors the table's `DEFAULT ''`. `provenanceId` is the owning Project
 * (use the sentinel `0` for "no attribution") and `agentId` records who
 * registered the artifact.
 *
 * @example
 * ```typescript
 * const args = CreateArtifactInput.parse({
 *     title: 'design.md', filepath: 'docs/design.md',
 *     provenanceId: 1, agentId: 1,
 * });
 * ```
 */
export const CreateArtifactInput = z.object({
    title:        z.string().min(1),
    description:  z.string().default(''),
    filepath:     z.string().default(''),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
});
export type CreateArtifactInput = z.infer<typeof CreateArtifactInput>;

/**
 * Input for `ArtifactCommands.update`.
 *
 * `sp_Artifact_Update` rewrites every text column unconditionally — the
 * caller is responsible for passing the canonical (already-merged) values
 * for every field. `relevance_status` is intentionally absent: change it
 * via `setRelevance` / `softDelete` / `restore` so the state-machine
 * audit trail stays accurate.
 *
 * @example
 * ```typescript
 * const args = UpdateArtifactInput.parse({
 *     artifactId: 1, title: 'design.md',
 *     description: 'system design', filepath: 'docs/design.md',
 *     reason: 'expanded scope section',
 * });
 * ```
 */
export const UpdateArtifactInput = z.object({
    artifactId:  z.number().int().positive(),
    title:       z.string().min(1),
    description: z.string(),
    filepath:    z.string(),
    reason:      z.string(),
});
export type UpdateArtifactInput = z.infer<typeof UpdateArtifactInput>;

/**
 * Input for `ArtifactCommands.setRelevance`.
 *
 * Drives the state-machine transition through `sp_Artifact_SetRelevance`,
 * which validates the (current → new) edge against `RelevanceStatus_Allowed`
 * and writes a row to `Artifact_StateTransition`. `newRelevanceStatus` is
 * locked to the five reference values seeded in `RelevanceStatus`.
 *
 * @example
 * ```typescript
 * const args = SetRelevanceInput.parse({
 *     artifactId: 1, newRelevanceStatus: 'superseded',
 *     agentId: 1, reason: 'replaced by design-v2.md',
 * });
 * ```
 */
export const SetRelevanceInput = z.object({
    artifactId:          z.number().int().positive(),
    newRelevanceStatus:  z.enum([
        'active',
        'needs-review',
        'superseded',
        'irrelevant',
        'deleted',
    ]),
    agentId:             z.number().int().nonnegative(),
    reason:              z.string(),
});
export type SetRelevanceInput = z.infer<typeof SetRelevanceInput>;

/**
 * Input for `ArtifactCommands.softDelete`.
 *
 * `sp_Artifact_Delete` is a soft delete — it forwards to
 * `sp_Artifact_SetRelevance(artifact_id, 'deleted', ...)` so the row
 * stays queryable via `vw_Deleted_Artifact` and can be restored later.
 *
 * @example
 * ```typescript
 * const args = DeleteArtifactInput.parse({
 *     artifactId: 1, agentId: 1, reason: 'no longer relevant',
 * });
 * ```
 */
export const DeleteArtifactInput = z.object({
    artifactId: z.number().int().positive(),
    agentId:    z.number().int().nonnegative(),
    reason:     z.string(),
});
export type DeleteArtifactInput = z.infer<typeof DeleteArtifactInput>;

/**
 * Input for `ArtifactCommands.restore`.
 *
 * Mirror of `DeleteArtifactInput`. Forwards to
 * `sp_Artifact_SetRelevance(artifact_id, 'active', ...)`.
 *
 * @example
 * ```typescript
 * const args = RestoreArtifactInput.parse({
 *     artifactId: 1, agentId: 1, reason: 'turned out to still be useful',
 * });
 * ```
 */
export const RestoreArtifactInput = z.object({
    artifactId: z.number().int().positive(),
    agentId:    z.number().int().nonnegative(),
    reason:     z.string(),
});
export type RestoreArtifactInput = z.infer<typeof RestoreArtifactInput>;

/**
 * Input for `ArtifactCommands.attachMilestone` — links an Artifact to a
 * Milestone via `Milestone_Artifact`. Idempotent at the SQL layer.
 *
 * @example
 * ```typescript
 * const args = AttachArtifactToMilestoneInput.parse({
 *     artifactId: 1, milestoneId: 1,
 * });
 * ```
 */
export const AttachArtifactToMilestoneInput = z.object({
    artifactId:  z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type AttachArtifactToMilestoneInput = z.infer<typeof AttachArtifactToMilestoneInput>;

/**
 * Input for `ArtifactCommands.detachMilestone` — removes the join row.
 *
 * @example
 * ```typescript
 * const args = DetachArtifactFromMilestoneInput.parse({
 *     artifactId: 1, milestoneId: 1,
 * });
 * ```
 */
export const DetachArtifactFromMilestoneInput = z.object({
    artifactId:  z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type DetachArtifactFromMilestoneInput = z.infer<typeof DetachArtifactFromMilestoneInput>;

/**
 * Input for `ArtifactCommands.attachTask` — links an Artifact to a Task
 * via `Task_Artifact`. Tasks are identified by the composite key
 * (`milestoneId`, `taskNo`).
 *
 * @example
 * ```typescript
 * const args = AttachArtifactToTaskInput.parse({
 *     artifactId: 1, milestoneId: 1, taskNo: 3,
 * });
 * ```
 */
export const AttachArtifactToTaskInput = z.object({
    artifactId:  z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
});
export type AttachArtifactToTaskInput = z.infer<typeof AttachArtifactToTaskInput>;

/**
 * Input for `ArtifactCommands.detachTask` — removes the join row.
 *
 * @example
 * ```typescript
 * const args = DetachArtifactFromTaskInput.parse({
 *     artifactId: 1, milestoneId: 1, taskNo: 3,
 * });
 * ```
 */
export const DetachArtifactFromTaskInput = z.object({
    artifactId:  z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
});
export type DetachArtifactFromTaskInput = z.infer<typeof DetachArtifactFromTaskInput>;

/**
 * Pagination options for `ArtifactQueries.listActive` / `listDeleted`.
 *
 * Matches the project-wide convention: 50-row pages capped at 500.
 *
 * @example
 * ```typescript
 * const opts = ListArtifactOpts.parse({ limit: 20, offset: 0 });
 * ```
 */
export const ListArtifactOpts = z.object({
    limit:  z.number().int().positive().max(500).default(50),
    offset: z.number().int().nonnegative().default(0),
});
export type ListArtifactOpts = z.infer<typeof ListArtifactOpts>;
