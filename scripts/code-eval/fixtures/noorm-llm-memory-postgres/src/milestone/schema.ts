import { z } from 'zod';

/**
 * Input for `MilestoneCommands.create`.
 *
 * `title` is the only required text field; `content` and `reason` mirror the
 * table's `DEFAULT ''` so callers may omit them. `provenanceId` is the owning
 * Project; `agentId` records who opened the milestone. Status columns are not
 * accepted here — `sp_Milestone_Create` always seeds `'not-started'` /
 * `'active'`.
 *
 * @example
 * ```typescript
 * const args = CreateMilestoneInput.parse({
 *     title: 'Q1 launch', provenanceId: 1, agentId: 1,
 * });
 * ```
 */
export const CreateMilestoneInput = z.object({
    title:        z.string().min(1),
    content:      z.string().default(''),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
});
export type CreateMilestoneInput = z.infer<typeof CreateMilestoneInput>;

/**
 * Input for `MilestoneCommands.update`.
 *
 * `sp_Milestone_Update` rewrites the three text columns unconditionally —
 * the proc has no partial-update path — so the caller is responsible for
 * passing canonical (already-merged) values for every field. Status changes
 * go through `setTracking` / `setRelevance`, not here.
 *
 * @example
 * ```typescript
 * const args = UpdateMilestoneInput.parse({
 *     milestoneId: 1, title: 'Q1 launch (revised)',
 *     content: '...', reason: 'scope cut',
 * });
 * ```
 */
export const UpdateMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    title:       z.string().min(1),
    content:     z.string(),
    reason:      z.string(),
});
export type UpdateMilestoneInput = z.infer<typeof UpdateMilestoneInput>;

/**
 * Input for `MilestoneCommands.setTracking`.
 *
 * The enum mirrors `TrackingStatus` reference-table rows. The PG-side proc
 * still validates the (from, to) pair against `TrackingStatus_Allowed`, so
 * a value that's syntactically valid here may still be rejected at runtime
 * if the transition isn't permitted from the current state.
 *
 * @example
 * ```typescript
 * const args = SetTrackingInput.parse({
 *     milestoneId: 1, newTrackingStatus: 'in-progress',
 *     agentId: 1, reason: 'work started',
 * });
 * ```
 */
export const SetTrackingInput = z.object({
    milestoneId:       z.number().int().positive(),
    newTrackingStatus: z.enum([
        'not-started',
        'in-progress',
        'agent-review',
        'human-review',
        'needs-more-work',
        'done',
        'abandoned',
    ]),
    agentId:           z.number().int().nonnegative(),
    reason:            z.string(),
});
export type SetTrackingInput = z.infer<typeof SetTrackingInput>;

/**
 * Input for `MilestoneCommands.setRelevance`.
 *
 * The enum mirrors `RelevanceStatus` reference-table rows. As with tracking,
 * the PG-side proc validates the transition against `RelevanceStatus_Allowed`
 * — a syntactically valid value can still be refused if the move isn't
 * permitted from the current state.
 *
 * @example
 * ```typescript
 * const args = SetRelevanceInput.parse({
 *     milestoneId: 1, newRelevanceStatus: 'superseded',
 *     agentId: 1, reason: 'rolled into next quarter',
 * });
 * ```
 */
export const SetRelevanceInput = z.object({
    milestoneId:        z.number().int().positive(),
    newRelevanceStatus: z.enum([
        'active',
        'needs-review',
        'superseded',
        'irrelevant',
        'deleted',
    ]),
    agentId:            z.number().int().nonnegative(),
    reason:             z.string(),
});
export type SetRelevanceInput = z.infer<typeof SetRelevanceInput>;

/**
 * Input for `MilestoneCommands.softDelete`.
 *
 * `sp_Milestone_Delete` is a soft delete — it routes through SetRelevance
 * with `'deleted'` and cascades into Notes attached via `Milestone_Note` and
 * `Task_Note`. The on-disk row is only removed when `sp_Cleanup` runs past
 * the TTL.
 *
 * @example
 * ```typescript
 * const args = DeleteMilestoneInput.parse({
 *     milestoneId: 1, agentId: 1, reason: 'duplicate',
 * });
 * ```
 */
export const DeleteMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId:     z.number().int().nonnegative(),
    reason:      z.string(),
});
export type DeleteMilestoneInput = z.infer<typeof DeleteMilestoneInput>;

/**
 * Input for `MilestoneCommands.restore`.
 *
 * Wrapper over `sp_Milestone_Restore`, which calls SetRelevance with
 * `'active'`. The transition is rejected if the milestone isn't currently
 * `'deleted'` (or whatever `RelevanceStatus_Allowed` permits).
 *
 * @example
 * ```typescript
 * const args = RestoreMilestoneInput.parse({
 *     milestoneId: 1, agentId: 1, reason: 'restore — still relevant',
 * });
 * ```
 */
export const RestoreMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId:     z.number().int().nonnegative(),
    reason:      z.string(),
});
export type RestoreMilestoneInput = z.infer<typeof RestoreMilestoneInput>;

/**
 * Input for `MilestoneCommands.close`.
 *
 * `sp_Milestone_Close` is a transactional batch: SetTracking → `'done'`,
 * SetRelevance → `'superseded'`, plus SetTracking → `'abandoned'` for every
 * still-open Task underneath. Each underlying proc validates its own
 * transition; if any of them is rejected the whole close is rolled back.
 *
 * @example
 * ```typescript
 * const args = CloseMilestoneInput.parse({
 *     milestoneId: 1, agentId: 1, reason: 'shipped',
 * });
 * ```
 */
export const CloseMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId:     z.number().int().nonnegative(),
    reason:      z.string(),
});
export type CloseMilestoneInput = z.infer<typeof CloseMilestoneInput>;

/**
 * Input for `MilestoneCommands.attachProject`.
 *
 * Inserts into `Project_Milestone`. The proc is idempotent at the SQL layer
 * (`ON CONFLICT DO NOTHING`), so re-attaching is a no-op rather than an error.
 *
 * @example
 * ```typescript
 * const args = AttachMilestoneToProjectInput.parse({
 *     milestoneId: 1, projectId: 2,
 * });
 * ```
 */
export const AttachMilestoneToProjectInput = z.object({
    milestoneId: z.number().int().positive(),
    projectId:   z.number().int().nonnegative(),
});
export type AttachMilestoneToProjectInput = z.infer<typeof AttachMilestoneToProjectInput>;

/**
 * Input for `MilestoneCommands.detachProject`.
 *
 * Deletes from `Project_Milestone`. Idempotent — detaching a pair that
 * doesn't exist is a no-op.
 *
 * @example
 * ```typescript
 * const args = DetachMilestoneFromProjectInput.parse({
 *     milestoneId: 1, projectId: 2,
 * });
 * ```
 */
export const DetachMilestoneFromProjectInput = z.object({
    milestoneId: z.number().int().positive(),
    projectId:   z.number().int().nonnegative(),
});
export type DetachMilestoneFromProjectInput = z.infer<typeof DetachMilestoneFromProjectInput>;

/**
 * Options accepted by the Milestone list / stats queries.
 *
 * Defaults match the project-wide convention: 50-row pages capped at 500.
 *
 * @example
 * ```typescript
 * const opts = ListMilestoneOpts.parse({ limit: 20 });
 * ```
 */
export const ListMilestoneOpts = z.object({
    limit:  z.number().int().positive().max(500).default(50),
    offset: z.number().int().nonnegative().default(0),
});
export type ListMilestoneOpts = z.infer<typeof ListMilestoneOpts>;
