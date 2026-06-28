/**
 * DQL for the Note domain.
 *
 * Read paths use Kysely directly so callers can compose narrower
 * projections when they need them. The defaults below select the full
 * row (or full view shape) for ergonomic single-row lookups and lists.
 *
 * @example
 * const notes = new NoteQueries(ctx);
 * const one = await notes.findById(1);
 * const view = await notes.findInView(1);
 * const all = await notes.list();
 */
import { Repo } from '../core/repo';

export class NoteQueries extends Repo {

    /** Fetch a single base Note row by id, or undefined when no row matches. */
    async findById(noteId: number) {

        return this.ctx.kysely
            .selectFrom('Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

    }

    /** Fetch the resolved vw_Note row (basetype + subtype keys) by id. */
    async findInView(noteId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Note')
            .selectAll()
            .where('note_id', '=', noteId)
            .executeTakeFirst();

    }

    /** List every Note ordered by id ascending. */
    async list() {

        return this.ctx.kysely
            .selectFrom('Note')
            .selectAll()
            .orderBy('note_id', 'asc')
            .execute();

    }

    /** List vw_Note rows (resolved subtype keys) ordered by id ascending. */
    async listInView() {

        return this.ctx.kysely
            .selectFrom('vw_Note')
            .selectAll()
            .orderBy('note_id', 'asc')
            .execute();

    }

}
