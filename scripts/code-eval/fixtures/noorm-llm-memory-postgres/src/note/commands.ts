import { Repo } from '../core/repo';

import {
    CreateMilestoneNoteInput,
    CreateProjectNoteInput,
    CreateTaskNoteInput,
    DeleteNoteInput,
    RestoreNoteInput,
    SetRelevanceInput,
    UpdateNoteInput,
} from './schema';

/**
 * Normalize the variable shape returned by the three `sp_Note_Create_*` procs
 * into a plain `note_id`.
 *
 * PostgreSQL exposes a `RETURNS INT` function as a single-column row whose
 * column is *named after the function* (e.g. `sp_Note_Create_Project`) unless
 * the function explicitly aliases the output. The `@noormdev/sdk` proc helper
 * preserves whatever PG gives back, so callers see either a `{ note_id }`
 * shape (preferred / aliased) or a `{ sp_Note_Create_* }` shape. This helper
 * accepts both and throws a descriptive error if it gets something else —
 * the caller can read the message to diagnose a schema drift.
 *
 * @example
 * ```typescript
 * const result = await this.ctx.proc('sp_Note_Create_Project', args);
 * return extractId(result);
 * ```
 */
function extractId(result: readonly unknown[]): number {

    const first = result[0];

    if (first && typeof first === 'object') {

        // cast-justified: SDK proc return is `{ note_id: number }[]` per the
        // type contract, but PG's RETURNS INT surfaces the value under a
        // function-named column at runtime. We accept either shape, so the
        // row is genuinely Record<string, unknown> until normalized.
        const obj = first as Record<string, unknown>; // cast-justified: see comment above

        const value = obj.note_id ?? Object.values(obj)[0];

        if (typeof value === 'number') {

            return value;

        }

    }

    throw new Error('createNote: unexpected proc return shape: ' + JSON.stringify(result));

}

/**
 * Write-side operations for the Note domain.
 *
 * Wraps the seven `sp_Note_*` procs. Every method validates input through
 * a Zod schema (so `unknown` becomes a typed `args` value), maps the
 * camelCase fields onto snake_case proc parameters, and awaits the proc
 * call. Errors propagate — there's no per-method try/catch.
 *
 * @example
 * ```typescript
 * const noteId = await db.note.cmd.createTask({
 *     content: 'Spike unblocked.', reason: 'progress',
 *     provenanceId: 1, agentId: 1, milestoneId: 7, taskNo: 3,
 * });
 * ```
 */
export class NoteCommands extends Repo {

    /**
     * Create a project-scoped Note and return its id.
     *
     * Inserts a `Note` row (`note_type = 'project'`, `relevance_status =
     * 'active'`) plus its matching `Project_Note` subtype row atomically.
     *
     * @example
     * ```typescript
     * const noteId = await db.note.cmd.createProject({
     *     content: 'Kickoff doc posted.', reason: 'planning',
     *     provenanceId: 1, agentId: 1, projectId: 1,
     * });
     * ```
     */
    async createProject(input: unknown): Promise<number> {

        const args = CreateProjectNoteInput.parse(input);

        const result = await this.ctx.proc('sp_Note_Create_Project', {
            p_content:       args.content,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
            p_project_id:    args.projectId,
        });

        return extractId(result);

    }

    /**
     * Create a milestone-scoped Note and return its id.
     *
     * Inserts a `Note` row (`note_type = 'milestone'`) plus its matching
     * `Milestone_Note` subtype row atomically.
     *
     * @example
     * ```typescript
     * const noteId = await db.note.cmd.createMilestone({
     *     content: 'M1 scope frozen.', reason: 'review',
     *     provenanceId: 1, agentId: 1, milestoneId: 7,
     * });
     * ```
     */
    async createMilestone(input: unknown): Promise<number> {

        const args = CreateMilestoneNoteInput.parse(input);

        const result = await this.ctx.proc('sp_Note_Create_Milestone', {
            p_content:       args.content,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
            p_milestone_id:  args.milestoneId,
        });

        return extractId(result);

    }

    /**
     * Create a task-scoped Note and return its id.
     *
     * Inserts a `Note` row (`note_type = 'task'`) plus its matching
     * `Task_Note` subtype row (composite key: `milestone_id` + `task_no`)
     * atomically.
     *
     * @example
     * ```typescript
     * const noteId = await db.note.cmd.createTask({
     *     content: 'Spike unblocked.', reason: 'progress',
     *     provenanceId: 1, agentId: 1, milestoneId: 7, taskNo: 3,
     * });
     * ```
     */
    async createTask(input: unknown): Promise<number> {

        const args = CreateTaskNoteInput.parse(input);

        const result = await this.ctx.proc('sp_Note_Create_Task', {
            p_content:       args.content,
            p_reason:        args.reason,
            p_provenance_id: args.provenanceId,
            p_agent_id:      args.agentId,
            p_milestone_id:  args.milestoneId,
            p_task_no:       args.taskNo,
        });

        return extractId(result);

    }

    /**
     * Update a Note's `content` and `reason`.
     *
     * `note_type` and the subtype attachment are immutable; relevance
     * changes flow through `setRelevance`. The proc rewrites both fields
     * unconditionally (no partial-update path).
     *
     * @example
     * ```typescript
     * await db.note.cmd.update({ noteId: 12, content: 'Edited.', reason: 'fix typo' });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Update', {
            p_note_id: args.noteId,
            p_content: args.content,
            p_reason:  args.reason,
        });

    }

    /**
     * Drive the relevance state machine for a Note.
     *
     * The (current, new) pair must be in `RelevanceStatus_Allowed` or the
     * proc raises. The transition is recorded via `Note_StateTransition`.
     *
     * @example
     * ```typescript
     * await db.note.cmd.setRelevance({
     *     noteId: 12, newRelevanceStatus: 'superseded',
     *     agentId: 1, reason: 'replaced by note 14',
     * });
     * ```
     */
    async setRelevance(input: unknown): Promise<void> {

        const args = SetRelevanceInput.parse(input);

        await this.ctx.proc('sp_Note_SetRelevance', {
            p_note_id:              args.noteId,
            p_new_relevance_status: args.newRelevanceStatus,
            p_agent_id:             args.agentId,
            p_reason:               args.reason,
        });

    }

    /**
     * Soft-delete a Note (transition `relevance_status` to `'deleted'`).
     *
     * Named `softDelete` to dodge `delete` as a reserved word and to
     * signal that the row remains queryable via `vw_Deleted_Note`.
     *
     * @example
     * ```typescript
     * await db.note.cmd.softDelete({ noteId: 12, agentId: 1, reason: 'duplicate' });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Delete', {
            p_note_id:  args.noteId,
            p_agent_id: args.agentId,
            p_reason:   args.reason,
        });

    }

    /**
     * Restore a soft-deleted Note (transition back to `'active'`).
     *
     * Subject to the same state-machine guard as any other relevance change.
     *
     * @example
     * ```typescript
     * await db.note.cmd.restore({ noteId: 12, agentId: 1, reason: 'mistaken delete' });
     * ```
     */
    async restore(input: unknown): Promise<void> {

        const args = RestoreNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Restore', {
            p_note_id:  args.noteId,
            p_agent_id: args.agentId,
            p_reason:   args.reason,
        });

    }

}
