/**
 * DQL for the Project domain.
 *
 * Uses Kysely directly so callers can compose narrower projections when
 * needed. The defaults below select everything for ergonomic single-row
 * lookups and full listings.
 *
 * @example
 * const projects = new ProjectQueries(ctx);
 * const mine = await projects.list();
 * const one = await projects.findById(1);
 */
import { Repo } from '../core/repo';

export class ProjectQueries extends Repo {

    /** Fetch a single Project by id, or undefined when no row matches. */
    async findById(projectId: number) {

        return this.ctx.kysely
            .selectFrom('Project')
            .selectAll()
            .where('project_id', '=', projectId)
            .executeTakeFirst();

    }

    /** List every Project ordered by id (sentinel 0 first). */
    async list() {

        return this.ctx.kysely
            .selectFrom('Project')
            .selectAll()
            .orderBy('project_id', 'asc')
            .execute();

    }

}
