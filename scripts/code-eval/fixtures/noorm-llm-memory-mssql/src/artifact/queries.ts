/**
 * DQL for the Artifact domain.
 *
 * Reads use Kysely directly so callers can compose narrower projections.
 * `list()` reads from vw_Active_Artifact so soft-deleted rows are filtered
 * out by default; callers needing the full table or the deleted view can
 * run their own selectFrom against this.ctx.kysely.
 *
 * @example
 * const artifacts = new ArtifactQueries(ctx);
 * const active = await artifacts.list();
 * const one = await artifacts.findById(1);
 */
import { Repo } from '../core/repo';

export class ArtifactQueries extends Repo {

    /** Fetch a single Artifact by id (any relevance), or undefined. */
    async findById(artifactId: number) {

        return this.ctx.kysely
            .selectFrom('Artifact')
            .selectAll()
            .where('artifact_id', '=', artifactId)
            .executeTakeFirst();

    }

    /** List active Artifacts (relevance_status = 'active') ordered by id. */
    async list() {

        return this.ctx.kysely
            .selectFrom('Artifact')
            .selectAll()
            .where('relevance_status', '=', 'active')
            .orderBy('artifact_id', 'asc')
            .execute();

    }

}
