import { Repo } from '../core/repo';

import {
    ListMemoryOpts,
    SearchMemoryOpts,
} from './schema';

/**
 * Read-side API for the `Memory` domain.
 *
 * Reads come from the curated views in `sql/07_views/`:
 *
 *   - `vw_Memory`         — every memory plus computed `confidence`.
 *   - `vw_Active_Memory`  — same shape, filtered to `relevance_status = 'active'`.
 *   - `vw_Deleted_Memory` — soft-delete recovery surface.
 *   - `vw_Related_Memory` — symmetric relationships (verb correctly
 *                            oriented per query side).
 *
 * Scalar functions (`fn_MemoryConfidence`, `fn_MemoryRank`) are reached
 * through `ctx.func()`; the third argument is the column alias the
 * function should be projected as. Each helper unwraps the
 * `{ <alias>: number }` shape into a plain `number` for the caller.
 *
 * @example
 * ```typescript
 * const memory     = await db.memory.q.findById(42);
 * const active     = await db.memory.q.listActive({ domain: 'backend' });
 * const recovery   = await db.memory.q.listDeleted({});
 * const mineToday  = await db.memory.q.listByAgent(7, {});
 * const score      = await db.memory.q.confidence(42);   // 0-4
 * const rank       = await db.memory.q.rank(42);         // 0.0-1.0
 * const links      = await db.memory.q.related(42);
 * const matches    = await db.memory.q.search({ contentLike: '%kysely%' });
 * ```
 */
export class MemoryQueries extends Repo {

    /**
     * Look up a single memory by id, with computed `confidence`
     * already projected by `vw_Memory`. Returns `undefined` when the
     * id does not match a row (including soft-deleted rows — those
     * are still in `vw_Memory`, only `vw_Active_Memory` filters them
     * out).
     *
     * @example
     * ```typescript
     * const memory = await db.memory.q.findById(42);
     * if (!memory) throw new Error('memory not found');
     * console.log(memory.confidence); // 0-4
     * ```
     */
    async findById(memoryId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .executeTakeFirst();

    }

    /**
     * Paginate active memories, optionally filtered by `domain` or
     * `category`. Defaults to the most-recently-updated page first.
     *
     * @example
     * ```typescript
     * const recent = await db.memory.q.listActive({ limit: 25 });
     * const facts  = await db.memory.q.listActive({ domain: 'backend', category: 'fact' });
     * ```
     */
    async listActive(input: unknown) {

        const opts = ListMemoryOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Active_Memory')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.domain) {

            q = q.where('domain', '=', opts.domain);
        }

        if (opts.category) {

            q = q.where('category', '=', opts.category);
        }

        return q.execute();

    }

    /**
     * Browse the soft-delete recovery surface — memories with
     * `relevance_status = 'deleted'` that `sp_Cleanup` will eventually
     * hard-delete. Same filter / pagination shape as `listActive` so
     * callers can reuse their UI.
     *
     * @example
     * ```typescript
     * const recoverable = await db.memory.q.listDeleted({ limit: 25 });
     * ```
     */
    async listDeleted(input: unknown) {

        const opts = ListMemoryOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Deleted_Memory')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.domain) {

            q = q.where('domain', '=', opts.domain);
        }

        if (opts.category) {

            q = q.where('category', '=', opts.category);
        }

        return q.execute();

    }

    /**
     * Paginate active memories authored by a specific agent.
     * Convenience for "what has this agent recorded lately?"
     * dashboards. Same filter / pagination shape as `listActive`.
     *
     * @example
     * ```typescript
     * const mine = await db.memory.q.listByAgent(7, { limit: 25 });
     * ```
     */
    async listByAgent(agentId: number, input: unknown) {

        const opts = ListMemoryOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Active_Memory')
            .selectAll()
            .where('agent_id', '=', agentId)
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.domain) {

            q = q.where('domain', '=', opts.domain);
        }

        if (opts.category) {

            q = q.where('category', '=', opts.category);
        }

        return q.execute();

    }

    /**
     * Compute the confidence score (0-4) — the count of `was_*`
     * booleans set to `true` on the row. Each flag is a verifiable
     * claim, so this number is a defensible signal rather than an
     * arbitrary score. Internally calls `fn_MemoryConfidence`.
     *
     * @example
     * ```typescript
     * const score = await db.memory.q.confidence(42);
     * if (score < 2) console.warn('low-confidence memory');
     * ```
     */
    async confidence(memoryId: number): Promise<number> {

        const result = await this.ctx.func('fn_MemoryConfidence', { p_memory_id: memoryId }, 'confidence');

        return result.confidence;

    }

    /**
     * Compute the composite retrieval rank — confidence weighted by
     * recency decay and relevance status. Use as `ORDER BY
     * fn_MemoryRank(memory_id) DESC` when keyword / domain / category
     * filters return too many candidates. Internally calls
     * `fn_MemoryRank`.
     *
     * @example
     * ```typescript
     * const rank = await db.memory.q.rank(42);
     * ```
     */
    async rank(memoryId: number): Promise<number> {

        const result = await this.ctx.func('fn_MemoryRank', { p_memory_id: memoryId }, 'rank');

        return result.rank;

    }

    /**
     * List every memory related to `memoryId` with the verb correctly
     * oriented from this memory's perspective. `vw_Related_Memory`
     * stores one direction per row in `Related_Memory` and exposes
     * both via UNION, swapping in `verb_backward` on the inverted
     * branch so callers never have to reason about direction.
     *
     * @example
     * ```typescript
     * const links = await db.memory.q.related(42);
     * for (const link of links) {
     *     console.log(`${link.verb} -> ${link.related_memory_id}`);
     * }
     * ```
     */
    async related(memoryId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Related_Memory')
            .selectAll()
            .where('memory_id', '=', memoryId)
            .execute();

    }

    /**
     * Free-text search over active memories. `contentLike` is a
     * Kysely `LIKE` pattern — callers add `%` wildcards as needed.
     * `agentId` filters by author. Both filters are optional; with
     * neither, this paginates the full active-memory view.
     *
     * Pure SQL `LIKE` — no embeddings, no full-text index. For
     * ranked retrieval, combine with `MemoryQueries.rank` at the
     * caller.
     *
     * @example
     * ```typescript
     * const matches = await db.memory.q.search({
     *     contentLike: '%kysely%',
     *     agentId: 7,
     *     limit: 25,
     * });
     * ```
     */
    async search(input: unknown) {

        const opts = SearchMemoryOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Active_Memory')
            .selectAll()
            .orderBy('updated_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset);

        if (opts.contentLike) {

            q = q.where('content', 'like', opts.contentLike);
        }

        if (opts.agentId !== undefined) {

            q = q.where('agent_id', '=', opts.agentId);
        }

        return q.execute();

    }

}
