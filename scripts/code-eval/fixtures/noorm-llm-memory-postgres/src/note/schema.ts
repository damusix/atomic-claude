import { z } from 'zod';

/**
 * Allowed values for `relevance_status` — kept here (not imported from a
 * shared module) so this schema file stays self-contained.
 */
const RelevanceStatus = z.enum([
    'active',
    'needs-review',
    'superseded',
    'irrelevant',
    'deleted',
]);

/**
 * Input for `NoteCommands.createProject`.
 *
 * Creates a Note with `note_type = 'project'` plus its matching
 * `Project_Note` subtype row in a single atomic call. `content` must be
 * non-empty; `reason` defaults to `''` to mirror the proc's free-text slot.
 *
 * @example
 * ```typescript
 * const args = CreateProjectNoteInput.parse({
 *     content: 'Kickoff doc posted.', reason: 'planning',
 *     provenanceId: 1, agentId: 1, projectId: 1,
 * });
 * ```
 */
export const CreateProjectNoteInput = z.object({
    content:      z.string().min(1),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
    projectId:    z.number().int().positive(),
});
export type CreateProjectNoteInput = z.infer<typeof CreateProjectNoteInput>;

/**
 * Input for `NoteCommands.createMilestone`.
 *
 * Creates a Note with `note_type = 'milestone'` plus its matching
 * `Milestone_Note` subtype row in a single atomic call.
 *
 * @example
 * ```typescript
 * const args = CreateMilestoneNoteInput.parse({
 *     content: 'M1 scope frozen.', reason: 'review',
 *     provenanceId: 1, agentId: 1, milestoneId: 7,
 * });
 * ```
 */
export const CreateMilestoneNoteInput = z.object({
    content:      z.string().min(1),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
    milestoneId:  z.number().int().positive(),
});
export type CreateMilestoneNoteInput = z.infer<typeof CreateMilestoneNoteInput>;

/**
 * Input for `NoteCommands.createTask`.
 *
 * Creates a Note with `note_type = 'task'` plus its matching `Task_Note`
 * subtype row (composite key: `milestone_id` + `task_no`) in a single
 * atomic call.
 *
 * @example
 * ```typescript
 * const args = CreateTaskNoteInput.parse({
 *     content: 'Spike unblocked.', reason: 'progress',
 *     provenanceId: 1, agentId: 1, milestoneId: 7, taskNo: 3,
 * });
 * ```
 */
export const CreateTaskNoteInput = z.object({
    content:      z.string().min(1),
    reason:       z.string().default(''),
    provenanceId: z.number().int().nonnegative(),
    agentId:      z.number().int().nonnegative(),
    milestoneId:  z.number().int().positive(),
    taskNo:       z.number().int().positive(),
});
export type CreateTaskNoteInput = z.infer<typeof CreateTaskNoteInput>;

/**
 * Input for `NoteCommands.update`.
 *
 * `sp_Note_Update` rewrites `content` and `reason` unconditionally — there
 * is no partial-update path — so the caller is responsible for passing the
 * canonical (already-merged) values. `note_type` and the subtype attachment
 * are immutable; relevance changes go through `setRelevance`.
 *
 * @example
 * ```typescript
 * const args = UpdateNoteInput.parse({
 *     noteId: 12, content: 'Updated text.', reason: 'edit',
 * });
 * ```
 */
export const UpdateNoteInput = z.object({
    noteId:  z.number().int().positive(),
    content: z.string().min(1),
    reason:  z.string().default(''),
});
export type UpdateNoteInput = z.infer<typeof UpdateNoteInput>;

/**
 * Input for `NoteCommands.setRelevance`.
 *
 * Drives the relevance state machine: the (current, new) pair must be in
 * `RelevanceStatus_Allowed` or the proc raises. `agentId` and `reason`
 * are persisted to the audit trail via the `Note_StateTransition` subtype.
 *
 * @example
 * ```typescript
 * const args = SetRelevanceInput.parse({
 *     noteId: 12, newRelevanceStatus: 'superseded',
 *     agentId: 1, reason: 'replaced by note 14',
 * });
 * ```
 */
export const SetRelevanceInput = z.object({
    noteId:             z.number().int().positive(),
    newRelevanceStatus: RelevanceStatus,
    agentId:            z.number().int().nonnegative(),
    reason:             z.string().default(''),
});
export type SetRelevanceInput = z.infer<typeof SetRelevanceInput>;

/**
 * Input for `NoteCommands.softDelete`.
 *
 * `sp_Note_Delete` is a soft-delete: it transitions `relevance_status` to
 * `'deleted'` (audited via `Note_StateTransition`). The JS-side method is
 * named `softDelete` to dodge `delete` as a reserved word and to signal
 * intent.
 *
 * @example
 * ```typescript
 * const args = DeleteNoteInput.parse({
 *     noteId: 12, agentId: 1, reason: 'duplicate of 14',
 * });
 * ```
 */
export const DeleteNoteInput = z.object({
    noteId:   z.number().int().positive(),
    agentId:  z.number().int().nonnegative(),
    reason:   z.string().default(''),
});
export type DeleteNoteInput = z.infer<typeof DeleteNoteInput>;

/**
 * Input for `NoteCommands.restore`.
 *
 * Reverses a soft-delete by transitioning `relevance_status` from
 * `'deleted'` back to `'active'` (subject to the state-machine table).
 *
 * @example
 * ```typescript
 * const args = RestoreNoteInput.parse({
 *     noteId: 12, agentId: 1, reason: 'mistaken delete',
 * });
 * ```
 */
export const RestoreNoteInput = z.object({
    noteId:  z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason:  z.string().default(''),
});
export type RestoreNoteInput = z.infer<typeof RestoreNoteInput>;

/**
 * Read-options for the `NoteQueries.list*` family.
 *
 * `noteType` filters to `'project' | 'milestone' | 'task'` when supplied.
 * Pagination defaults to limit 50, capped at 500 to prevent runaway scans.
 *
 * @example
 * ```typescript
 * const opts = ListNoteOpts.parse({ noteType: 'task', limit: 20 });
 * ```
 */
export const ListNoteOpts = z.object({
    noteType: z.enum(['project', 'milestone', 'task']).optional(),
    limit:    z.number().int().positive().max(500).default(50),
    offset:   z.number().int().nonnegative().default(0),
});
export type ListNoteOpts = z.infer<typeof ListNoteOpts>;
