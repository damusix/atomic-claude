import { z } from 'zod';

/**
 * Allowed values for `tracking_status` — kept inline (not imported from a
 * shared module) so this schema file stays self-contained. Mirrors the seed
 * data in the `TrackingStatus` reference table; the proc layer additionally
 * gates transitions through `TrackingStatus_Allowed`.
 */
const TrackingStatus = z.enum([
    'not-started',
    'in-progress',
    'agent-review',
    'human-review',
    'needs-more-work',
    'done',
    'abandoned',
]);

/**
 * Allowed values for `dependency_verb` — mirrors the seed data in the
 * `DependencyVerb` reference table. The proc rejects unknown verbs at
 * insert time; validating here gives callers an earlier, friendlier error.
 */
const DependencyVerb = z.enum([
    'blocks',
    'requires',
    'informs',
    'follows',
    'extends',
    'duplicates',
]);

/**
 * Input for `TaskCommands.create`.
 *
 * `milestoneId` and `agentId` are required — every task lives under a
 * milestone and is attributed to an agent (use `0` for the "no
 * attribution" sentinel). `title` must be non-empty; `content` and
 * `reason` default to `''` to mirror the table's `DEFAULT ''` columns
 * and the proc's free-text slots.
 *
 * @example
 * ```typescript
 * const args = CreateTaskInput.parse({
 *     milestoneId: 7, title: 'Wire DT importer',
 *     content: '', reason: 'planning', agentId: 1,
 * });
 * ```
 */
export const CreateTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    title:       z.string().min(1),
    content:     z.string().default(''),
    reason:      z.string().default(''),
    agentId:     z.number().int().nonnegative(),
});
export type CreateTaskInput = z.infer<typeof CreateTaskInput>;

/**
 * Input for `TaskCommands.update`.
 *
 * `sp_Task_Update` rewrites every text column unconditionally — there is
 * no partial-update path — so callers must pass the canonical
 * (already-merged) values for `title`, `content`, and `reason`.
 * Tracking status changes are routed through `setTracking` instead.
 *
 * @example
 * ```typescript
 * const args = UpdateTaskInput.parse({
 *     milestoneId: 7, taskNo: 3,
 *     title: 'Wire DT importer (revised)',
 *     content: 'Use the new bridge API.',
 *     reason: 'scope clarified',
 * });
 * ```
 */
export const UpdateTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
    title:       z.string().min(1),
    content:     z.string(),
    reason:      z.string(),
});
export type UpdateTaskInput = z.infer<typeof UpdateTaskInput>;

/**
 * Input for `TaskCommands.setTracking`.
 *
 * Drives the tracking state machine: the (current, new) pair must be in
 * `TrackingStatus_Allowed` or the proc raises. `agentId` and `reason`
 * are persisted to the audit trail via `Task_StateTransition`.
 *
 * @example
 * ```typescript
 * const args = SetTrackingInput.parse({
 *     milestoneId: 7, taskNo: 3, newTrackingStatus: 'in-progress',
 *     agentId: 1, reason: 'started spike',
 * });
 * ```
 */
export const SetTrackingInput = z.object({
    milestoneId:       z.number().int().positive(),
    taskNo:            z.number().int().positive(),
    newTrackingStatus: TrackingStatus,
    agentId:           z.number().int().nonnegative(),
    reason:            z.string().default(''),
});
export type SetTrackingInput = z.infer<typeof SetTrackingInput>;

/**
 * Input for `TaskCommands.softDelete`.
 *
 * `sp_Task_Delete` soft-deletes every Note attached via `Task_Note`
 * (each routed through the relevance state machine), then transitions
 * the task's `tracking_status` to `'abandoned'`. The JS-side method is
 * named `softDelete` to dodge `delete` as a reserved word and to signal
 * that nothing is hard-deleted here.
 *
 * @example
 * ```typescript
 * const args = DeleteTaskInput.parse({
 *     milestoneId: 7, taskNo: 3, agentId: 1, reason: 'duplicate of task 5',
 * });
 * ```
 */
export const DeleteTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo:      z.number().int().positive(),
    agentId:     z.number().int().nonnegative(),
    reason:      z.string().default(''),
});
export type DeleteTaskInput = z.infer<typeof DeleteTaskInput>;

/**
 * Input for `TaskCommands.depend`.
 *
 * Records that `(milestoneId, taskNo)` depends on `(depMilestoneId,
 * depTaskNo)` with the chosen verb. The proc rejects self-dependencies
 * and any edge that would close a cycle (validated via
 * `fn_TaskDependencyWouldCycle`); the underlying insert is idempotent
 * (`ON CONFLICT DO NOTHING`), so re-issuing the same edge is a no-op.
 *
 * @example
 * ```typescript
 * const args = DependInput.parse({
 *     milestoneId: 7, taskNo: 3,
 *     depMilestoneId: 7, depTaskNo: 1,
 *     dependencyVerb: 'blocks', reason: 'needs schema first',
 * });
 * ```
 */
export const DependInput = z.object({
    milestoneId:    z.number().int().positive(),
    taskNo:         z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo:      z.number().int().positive(),
    dependencyVerb: DependencyVerb,
    reason:         z.string().default(''),
});
export type DependInput = z.infer<typeof DependInput>;

/**
 * Input for `TaskCommands.undepend`.
 *
 * Drops the dependency edge identified by the four-key tuple. Idempotent
 * — deleting an edge that doesn't exist is a silent no-op.
 *
 * @example
 * ```typescript
 * const args = UndependInput.parse({
 *     milestoneId: 7, taskNo: 3, depMilestoneId: 7, depTaskNo: 1,
 * });
 * ```
 */
export const UndependInput = z.object({
    milestoneId:    z.number().int().positive(),
    taskNo:         z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo:      z.number().int().positive(),
});
export type UndependInput = z.infer<typeof UndependInput>;

/**
 * Input for `TaskQueries.wouldCycle` — a read-side preview of the
 * cycle check `sp_Task_Depend` runs internally. Useful for surfacing
 * "this would create a cycle" in a planning UI without attempting the
 * insert.
 *
 * @example
 * ```typescript
 * const args = WouldCycleInput.parse({
 *     milestoneId: 7, taskNo: 3, depMilestoneId: 7, depTaskNo: 1,
 * });
 * ```
 */
export const WouldCycleInput = z.object({
    milestoneId:    z.number().int().positive(),
    taskNo:         z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo:      z.number().int().positive(),
});
export type WouldCycleInput = z.infer<typeof WouldCycleInput>;

/**
 * Read-options for the `TaskQueries.list*` family.
 *
 * `milestoneId` scopes the listing when supplied. `openOnly` (default
 * `true`) filters out tasks whose `tracking_status` is `'done'` or
 * `'abandoned'` — the common "what's left to do" view. Pagination
 * defaults to limit 50, capped at 500 to prevent runaway scans.
 *
 * @example
 * ```typescript
 * const opts = ListTaskOpts.parse({ milestoneId: 7, limit: 25 });
 * ```
 */
export const ListTaskOpts = z.object({
    milestoneId: z.number().int().positive().optional(),
    openOnly:    z.boolean().default(true),
    limit:       z.number().int().positive().max(500).default(50),
    offset:      z.number().int().nonnegative().default(0),
});
export type ListTaskOpts = z.infer<typeof ListTaskOpts>;
