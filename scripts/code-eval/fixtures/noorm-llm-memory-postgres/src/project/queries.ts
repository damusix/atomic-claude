import { z } from 'zod';

import { Repo } from '../core/repo';

/**
 * Options accepted by `ProjectQueries.list`.
 *
 * Inlined here (rather than living in `schema.ts`) because list options are
 * read-side concerns that don't map onto a stored procedure. Defaults match
 * the project-wide convention: 50-row pages capped at 500.
 */
const ListProjectOpts = z.object({
    limit:  z.number().int().positive().max(500).default(50),
    offset: z.number().int().nonnegative().default(0),
});

/**
 * Read-side API for the `Project` table.
 *
 * Reads go straight to the table via Kysely — there is no `vw_Project` and
 * no scalar function in the Project domain. List queries are always
 * pagination-bounded; lookups by id and by name are convenience wrappers
 * over the most common WHERE clauses.
 *
 * @example
 * ```typescript
 * const project = await db.project.q.findById(1);
 * const page    = await db.project.q.list({ limit: 20 });
 * const named   = await db.project.q.findByName('noorm');
 * ```
 */
export class ProjectQueries extends Repo {

    /**
     * Look a project up by its primary key. Returns `undefined` when the
     * id does not match a row (including the sentinel `0`, if missing).
     *
     * @example
     * ```typescript
     * const project = await db.project.q.findById(1);
     * ```
     */
    async findById(projectId: number) {

        return this.ctx.kysely
            .selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirst();

    }

    /**
     * Paginate Projects ordered alphabetically by name. Useful for picker
     * UIs where the user scrolls a registry of known projects.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.project.q.list({ limit: 20 });
     * const secondPage = await db.project.q.list({ limit: 20, offset: 20 });
     * ```
     */
    async list(input: unknown) {

        const opts = ListProjectOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('Project')
            .selectAll()
            .orderBy('name', 'asc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Look up the first Project whose `name` exactly matches. The schema
     * does not declare `name` as unique, so this returns the first match
     * by insertion order — sufficient for the conventional "one project
     * per repo" usage.
     *
     * @example
     * ```typescript
     * const project = await db.project.q.findByName('noorm');
     * ```
     */
    async findByName(name: string) {

        return this.ctx.kysely
            .selectFrom('Project')
            .selectAll()
            .where('name', '=', name)
            .executeTakeFirst();

    }

}
