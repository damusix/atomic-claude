import { z } from 'zod';

/**
 * Input for `TagCommands.create`.
 *
 * `name` is the unique key (enforced at the table) and is the only field a
 * caller must supply. `description` and `reason` mirror their `DEFAULT ''`
 * column defaults so a typical "just give me a tag named X" call site stays
 * a single line. `provenanceId` ties the tag to a Project; `agentId`
 * records the actor — pass `0` for the sentinel "no attribution" agent.
 *
 * `color` is intentionally NOT on the create input — the column defaults
 * to `''` server-side and is only set later via UI (no proc parameter
 * exists for it). Adding it here would imply a write path that doesn't
 * exist yet.
 *
 * @example
 * ```typescript
 * const args = CreateTagInput.parse({
 *     name: 'backend-performance', provenanceId: 1, agentId: 1,
 * });
 * ```
 */
export const CreateTagInput = z.object({
    name:         z.string().min(1).max(255),
    description:  z.string().default(''),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
});
export type CreateTagInput = z.infer<typeof CreateTagInput>;

/**
 * Input for `TagCommands.update`.
 *
 * `sp_Tag_Update` rewrites every text column unconditionally — there is no
 * partial-update path in the proc — so the caller must pass the canonical
 * (already-merged) values for every editable field.
 *
 * @example
 * ```typescript
 * await db.tag.cmd.update({
 *     tagId: 7, name: 'backend-perf',
 *     description: 'merged from backend-performance',
 *     reason: 'taxonomy cleanup',
 * });
 * ```
 */
export const UpdateTagInput = z.object({
    tagId:       z.number().int().positive(),
    name:        z.string().min(1).max(255),
    description: z.string(),
    reason:      z.string(),
});
export type UpdateTagInput = z.infer<typeof UpdateTagInput>;

/**
 * Input for `TagCommands.remove`.
 *
 * `sp_Tag_Delete` is a *hard* delete — Tags carry no `relevance_status`,
 * so there is no soft-delete option. `ON DELETE CASCADE` on every
 * `*_Tag` join table sweeps the attachments away atomically.
 *
 * The wrapper is named `remove` (not `softDelete`) to signal that the
 * row really is gone after the call returns.
 *
 * @example
 * ```typescript
 * await db.tag.cmd.remove({ tagId: 7 });
 * ```
 */
export const DeleteTagInput = z.object({
    tagId: z.number().int().positive(),
});
export type DeleteTagInput = z.infer<typeof DeleteTagInput>;

// ── Attach inputs (one per attachable entity kind) ──────────────────────────

/**
 * Input for `TagCommands.attachProject` — idempotent at the SQL layer
 * (`ON CONFLICT DO NOTHING`).
 */
export const AttachTagToProjectInput = z.object({
    tagId:     z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type AttachTagToProjectInput = z.infer<typeof AttachTagToProjectInput>;

/**
 * Input for `TagCommands.attachMemory` — idempotent.
 */
export const AttachTagToMemoryInput = z.object({
    tagId:    z.number().int().positive(),
    memoryId: z.number().int().positive(),
});
export type AttachTagToMemoryInput = z.infer<typeof AttachTagToMemoryInput>;

/**
 * Input for `TagCommands.attachArtifact` — idempotent.
 */
export const AttachTagToArtifactInput = z.object({
    tagId:      z.number().int().positive(),
    artifactId: z.number().int().positive(),
});
export type AttachTagToArtifactInput = z.infer<typeof AttachTagToArtifactInput>;

/**
 * Input for `TagCommands.attachMilestone` — idempotent.
 */
export const AttachTagToMilestoneInput = z.object({
    tagId:       z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type AttachTagToMilestoneInput = z.infer<typeof AttachTagToMilestoneInput>;

/**
 * Input for `TagCommands.attachTask` — idempotent.
 *
 * Tasks have a composite primary key (`milestone_id`, `task_no`), so both
 * components are required to identify the target row.
 */
export const AttachTagToTaskInput = z.object({
    tagId:       z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
});
export type AttachTagToTaskInput = z.infer<typeof AttachTagToTaskInput>;

// ── Detach inputs (mirror Attach exactly) ───────────────────────────────────

/**
 * Input for `TagCommands.detachProject` — idempotent (DELETE matches zero
 * or one row).
 */
export const DetachTagFromProjectInput = z.object({
    tagId:     z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type DetachTagFromProjectInput = z.infer<typeof DetachTagFromProjectInput>;

/**
 * Input for `TagCommands.detachMemory` — idempotent.
 */
export const DetachTagFromMemoryInput = z.object({
    tagId:    z.number().int().positive(),
    memoryId: z.number().int().positive(),
});
export type DetachTagFromMemoryInput = z.infer<typeof DetachTagFromMemoryInput>;

/**
 * Input for `TagCommands.detachArtifact` — idempotent.
 */
export const DetachTagFromArtifactInput = z.object({
    tagId:      z.number().int().positive(),
    artifactId: z.number().int().positive(),
});
export type DetachTagFromArtifactInput = z.infer<typeof DetachTagFromArtifactInput>;

/**
 * Input for `TagCommands.detachMilestone` — idempotent.
 */
export const DetachTagFromMilestoneInput = z.object({
    tagId:       z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type DetachTagFromMilestoneInput = z.infer<typeof DetachTagFromMilestoneInput>;

/**
 * Input for `TagCommands.detachTask` — idempotent. Composite task PK.
 */
export const DetachTagFromTaskInput = z.object({
    tagId:       z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
});
export type DetachTagFromTaskInput = z.infer<typeof DetachTagFromTaskInput>;

/**
 * Input for `TagCommands.merge`.
 *
 * Re-points every `*_Tag` row that references `sourceTagId` to
 * `targetTagId` (skipping rows that would duplicate an existing target
 * attachment), then hard-deletes the source. Use case: collapsing
 * synonymous tags discovered after the fact (e.g. `backend-perf` and
 * `backend-performance` are the same concept).
 *
 * `agentId` and `reason` are recorded on the audit trail so the merge
 * decision is traceable. The proc rejects `sourceTagId == targetTagId`
 * and rejects either side missing.
 *
 * @example
 * ```typescript
 * await db.tag.cmd.merge({
 *     sourceTagId: 12, targetTagId: 7, agentId: 1,
 *     reason: 'consolidating duplicate taxonomy entries',
 * });
 * ```
 */
export const MergeTagInput = z.object({
    sourceTagId: z.number().int().positive(),
    targetTagId: z.number().int().positive(),
    agentId:     z.number().int().nonnegative(),
    reason:      z.string().default(''),
});
export type MergeTagInput = z.infer<typeof MergeTagInput>;
