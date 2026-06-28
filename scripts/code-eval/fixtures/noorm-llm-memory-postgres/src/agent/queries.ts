import { Repo } from '../core/repo';

/**
 * Read surface for the `Agent` domain.
 *
 * Agents are a flat reference-style entity — there's no soft-delete view
 * pair to manage, so the query API is intentionally small:
 *
 *   - `findById` — direct lookup by primary key.
 *   - `list`     — paginated browse, ordered by name (the human-friendly key).
 *   - `activity` — per-agent rollup from `vw_Agent_Activity`
 *                  (memories/notes/tasks created, transitions made, etc.).
 *
 * @example
 * ```typescript
 * const agent    = await db.agent.qry.findById(7);
 * const page     = await db.agent.qry.list({ limit: 25 });
 * const activity = await db.agent.qry.activity(7);
 * ```
 */
export class AgentQueries extends Repo {

    /**
     * Look up a single agent by its primary key.
     *
     * Returns `undefined` when no row matches — callers decide whether
     * that's an error or a normal "not registered yet" signal.
     *
     * @example
     * ```typescript
     * const agent = await db.agent.qry.findById(7);
     * if (!agent) throw new Error('agent not registered');
     * ```
     */
    async findById(agentId: number) {

        return this.ctx.kysely
            .selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirst();

    }

    /**
     * List agents alphabetically, with offset-based pagination.
     *
     * Defaults: `limit = 50`, `offset = 0`. Capped at 500 to keep
     * accidental "list everything" calls from sweeping the whole table.
     *
     * @example
     * ```typescript
     * const firstPage  = await db.agent.qry.list({});
     * const secondPage = await db.agent.qry.list({ limit: 50, offset: 50 });
     * ```
     */
    async list(opts: { limit?: number; offset?: number }) {

        const limit  = Math.min(opts.limit ?? 50, 500);
        const offset = opts.offset ?? 0;

        return this.ctx.kysely
            .selectFrom('Agent')
            .selectAll()
            .orderBy('name', 'asc')
            .limit(limit)
            .offset(offset)
            .execute();

    }

    /**
     * Fetch the per-agent rollup from `vw_Agent_Activity` — counts of
     * memories / notes / artifacts / milestones / tasks / tags created,
     * total transitions made, and the timestamp of the most recent action.
     *
     * Returns `undefined` when the agent has no rows in any tracked table
     * (typical for a freshly registered agent that hasn't acted yet).
     *
     * @example
     * ```typescript
     * const stats = await db.agent.qry.activity(7);
     * console.log(stats?.memories_created, stats?.last_action_at);
     * ```
     */
    async activity(agentId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Agent_Activity')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirst();

    }

}
