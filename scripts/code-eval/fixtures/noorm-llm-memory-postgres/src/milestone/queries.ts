import { Repo } from '../core/repo';

import { ListMilestoneOpts } from './schema';

/**
 * Read-side API for the `Milestone` table and its views.
 *
 * Reads either go straight to `Milestone` via Kysely (single-row lookups)
 * or hit one of the project's filtered / aggregated views:
 *
 * - `vw_Active_Milestone` / `vw_Deleted_Milestone` — the relevance-filtered
 *   browsing surfaces (saves a `WHERE relevance_status = ...` everywhere).
 * - `vw_Milestone_Stats` — per-milestone aggregate counts (tasks by status,
 *   notes, tags, artifacts, dependencies, project_count).
 *
 * List queries are always pagination-bounded (default 50, max 500).
 *
 * @example
 * ```typescript
 * const m       = await db.milestone.q.findById(1);
 * const stats   = await db.milestone.q.stats(1);
 * const open    = await db.milestone.q.listActive({ limit: 20 });
 * const trash   = await db.milestone.q.listDeleted({ limit: 20 });
 * const inProj  = await db.milestone.q.listForProject(2, { limit: 20 });
 * const allAgg  = await db.milestone.q.listAllStats({ limit: 20 });
 * ```
 */
export class MilestoneQueries extends Repo {

    /**
     * Look a Milestone up by its primary key. Returns `undefined` when the
     * id does not match a row.
     *
     * @example
     * ```typescript
     * const m = await db.milestone.q.findById(1);
     * ```
     */
    async findById(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('Milestone')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

    }

    /**
     * Aggregate metrics for a single Milestone — task counts by tracking
     * status, plus rolled-up totals for artifacts, notes, tags, deps, and
     * the count of Projects this milestone belongs to.
     *
     * @example
     * ```typescript
     * const stats = await db.milestone.q.stats(1);
     * ```
     */
    async stats(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Milestone_Stats')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .executeTakeFirst();

    }

    /**
     * Paginate Milestones whose `relevance_status = 'active'`. The view
     * already has the WHERE clause baked in.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.milestone.q.listActive({ limit: 20 });
     * const secondPage = await db.milestone.q.listActive({ limit: 20, offset: 20 });
     * ```
     */
    async listActive(input: unknown) {

        const opts = ListMilestoneOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Active_Milestone')
            .selectAll()
            .orderBy('created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Paginate Milestones whose `relevance_status = 'deleted'`. Useful as a
     * recovery surface — the LLM can browse this to find soft-deleted
     * milestones worth `restore`-ing before `sp_Cleanup` hard-deletes them
     * past the TTL.
     *
     * @example
     * ```typescript
     * const trash = await db.milestone.q.listDeleted({ limit: 20 });
     * ```
     */
    async listDeleted(input: unknown) {

        const opts = ListMilestoneOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Deleted_Milestone')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Paginate Milestones associated with a given Project via
     * `Project_Milestone`. Inner-joins onto `Milestone` so callers get the
     * full row, not just the join keys.
     *
     * @example
     * ```typescript
     * const milestones = await db.milestone.q.listForProject(2, { limit: 20 });
     * ```
     */
    async listForProject(projectId: number, input: unknown) {

        const opts = ListMilestoneOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('Milestone')
            .innerJoin(
                'Project_Milestone',
                'Project_Milestone.milestone_id',
                'Milestone.milestone_id',
            )
            .selectAll('Milestone')
            .where('Project_Milestone.project_id', '=', projectId)
            .orderBy('Milestone.created_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Paginate the per-milestone stats view across every Milestone — the
     * dashboard read for "show me everyone's progress at a glance".
     *
     * @example
     * ```typescript
     * const dashboard = await db.milestone.q.listAllStats({ limit: 20 });
     * ```
     */
    async listAllStats(input: unknown) {

        const opts = ListMilestoneOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_Milestone_Stats')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

}
