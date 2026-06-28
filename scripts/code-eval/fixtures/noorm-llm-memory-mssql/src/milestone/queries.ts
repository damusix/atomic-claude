/**
 * DQL for the Milestone domain.
 *
 * Reads use Kysely directly so callers can compose narrower projections.
 * `list()` reads from vw_Active_Milestone so soft-deleted rows are filtered
 * out by default; callers needing the full table or the deleted view can
 * run their own selectFrom against this.ctx.kysely.
 *
 * `stats()` returns the rollup row from vw_Milestone_Stats — task counts,
 * artifact/note/tag counts, dependency counts, project count.
 *
 * @example
 * const milestones = new MilestoneQueries(ctx);
 * const active = await milestones.list();
 * const summary = await milestones.stats(1);
 */
import { Repo } from '../core/repo';

export class MilestoneQueries extends Repo {

    /** Fetch a single Milestone by id (any relevance), or undefined. */
    async findById(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

    }

    /** List active Milestones (relevance_status = 'active') ordered by id. */
    async list() {

        return this.ctx.kysely
            .selectFrom('Milestone')
            .selectAll()
            .where('relevance_status', '=', 'active')
            .orderBy('milestone_id', 'asc')
            .execute();

    }

    /** Fetch rollup stats from vw_Milestone_Stats for one milestone. */
    async stats(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Milestone_Stats')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

    }

}
