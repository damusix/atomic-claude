import { Repo } from '../core/repo';

import { ListNoteOpts } from './schema';

/**
 * Read-side operations for the Note domain.
 *
 * `findById` and the polymorphic listing methods read from `vw_Note`
 * (which surfaces the parent FK as `project_id` / `milestone_id` /
 * `task_no` columns regardless of subtype). `listActive` and
 * `listDeleted` read from the dedicated relevance-filtered views.
 *
 * @example
 * ```typescript
 * const note   = await db.note.q.findById(12);
 * const active = await db.note.q.listActive({ limit: 25 });
 * ```
 */
export class NoteQueries extends Repo {

    /**
     * Look up a single note by id via `vw_Note`.
     *
     * Returns the polymorphic view row — `project_id`, `milestone_id`,
     * and `task_no` are populated according to `note_type`; the others
     * are `0`. Returns `undefined` if no row matches.
     *
     * @example
     * ```typescript
     * const note = await db.note.q.findById(12);
     * if (!note) throw new Error('not found');
     * ```
     */
    async findById(noteId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

    }

    /**
     * Paginated list of active notes (`relevance_status = 'active'`).
     *
     * Optional `noteType` filter narrows to one subtype. Defaults to
     * `limit: 50`, capped at 500.
     *
     * @example
     * ```typescript
     * const notes = await db.note.q.listActive({ noteType: 'task', limit: 20 });
     * ```
     */
    async listActive(input: unknown) {

        const opts = ListNoteOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Active_Note')
            .selectAll()
            .orderBy('created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.noteType) {

            q = q.where('note_type', '=', opts.noteType);

        }

        return q.execute();

    }

    /**
     * Paginated list of soft-deleted notes (`relevance_status = 'deleted'`).
     *
     * Mirrors `listActive` but reads from `vw_Deleted_Note`.
     *
     * @example
     * ```typescript
     * const trashed = await db.note.q.listDeleted({ limit: 50 });
     * ```
     */
    async listDeleted(input: unknown) {

        const opts = ListNoteOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Deleted_Note')
            .selectAll()
            .orderBy('created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.noteType) {

            q = q.where('note_type', '=', opts.noteType);

        }

        return q.execute();

    }

    /**
     * Notes attached to a given Project via `Project_Note`.
     *
     * Joins `vw_Note` against `Project_Note` so callers get the full
     * polymorphic view row (with `project_id` already populated).
     *
     * @example
     * ```typescript
     * const projectNotes = await db.note.q.listByProject(1, { limit: 25 });
     * ```
     */
    async listByProject(projectId: number, input: unknown) {

        const opts = ListNoteOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Note')
            .innerJoin('Project_Note', 'Project_Note.note_id', 'vw_Note.note_id')
            .selectAll('vw_Note')
            .where('Project_Note.project_id', '=', projectId)
            .orderBy('vw_Note.created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.noteType) {

            q = q.where('vw_Note.note_type', '=', opts.noteType);

        }

        return q.execute();

    }

    /**
     * Notes attached to a given Milestone via `Milestone_Note`.
     *
     * @example
     * ```typescript
     * const milestoneNotes = await db.note.q.listByMilestone(7, { limit: 25 });
     * ```
     */
    async listByMilestone(milestoneId: number, input: unknown) {

        const opts = ListNoteOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Note')
            .innerJoin('Milestone_Note', 'Milestone_Note.note_id', 'vw_Note.note_id')
            .selectAll('vw_Note')
            .where('Milestone_Note.milestone_id', '=', milestoneId)
            .orderBy('vw_Note.created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.noteType) {

            q = q.where('vw_Note.note_type', '=', opts.noteType);

        }

        return q.execute();

    }

    /**
     * Notes attached to a given Task via `Task_Note`.
     *
     * Tasks have a composite key (`milestone_id` + `task_no`), so both
     * coordinates are required.
     *
     * @example
     * ```typescript
     * const taskNotes = await db.note.q.listByTask(7, 3, { limit: 25 });
     * ```
     */
    async listByTask(milestoneId: number, taskNo: number, input: unknown) {

        const opts = ListNoteOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Note')
            .innerJoin('Task_Note', 'Task_Note.note_id', 'vw_Note.note_id')
            .selectAll('vw_Note')
            .where('Task_Note.milestone_id', '=', milestoneId)
            .where('Task_Note.task_no', '=', taskNo)
            .orderBy('vw_Note.created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.noteType) {

            q = q.where('vw_Note.note_type', '=', opts.noteType);

        }

        return q.execute();

    }

}
