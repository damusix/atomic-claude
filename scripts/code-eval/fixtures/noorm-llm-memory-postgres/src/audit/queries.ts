import { sql } from 'kysely';

import { Repo } from '../core/repo';
import {
    AgentHistoryOpts,
    ArtifactHistoryOpts,
    MemoryHistoryOpts,
    MilestoneHistoryOpts,
    NoteHistoryOpts,
    RecentActivityOpts,
    RecoveriesOpts,
    TaskHistoryOpts,
} from './schema';

/**
 * Read-only API for the audit subsystem.
 *
 * Every method reads from one of the three audit views:
 *
 *   - `vw_StateTransition`  — unified transition log with the entity
 *                             columns flattened in (one entity column
 *                             populated per row, the rest `0`).
 *   - `vw_Recent_Activity`  — cross-entity stream of recent creates,
 *                             updates, and transitions.
 *   - `vw_Agent_Activity`   — per-agent rollup of counts plus the
 *                             timestamp of the most recent action.
 *
 * The audit subsystem is append-only — there are no `Audit*` stored
 * procedures, hence no `commands.ts`. All transition rows are produced
 * as a side effect of mutating procs in the other domains.
 *
 * @example
 * ```typescript
 * const milestoneLog = await db.audit.qry.milestoneHistory({ milestoneId: 7 });
 * const taskLog      = await db.audit.qry.taskHistory({ milestoneId: 7, taskNo: 3 });
 * const recoveries   = await db.audit.qry.recoveries({ sinceDays: 30 });
 * ```
 */
export class AuditQueries extends Repo {

    /**
     * Lifecycle of a single milestone — only milestone-level transitions
     * (tracking + relevance), not per-task rows. The `task_no = 0` filter
     * is what excludes task transitions, since `vw_StateTransition` packs
     * task rows into the same milestone bucket.
     *
     * Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.milestoneHistory({ milestoneId: 7 });
     * ```
     */
    async milestoneHistory(input: unknown) {

        const opts = MilestoneHistoryOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('milestone_id', '=', opts.milestoneId)
            .where('task_no', '=', 0)
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Lifecycle of a single task — tracking-status transitions for the
     * given (`milestone_id`, `task_no`) pair. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.taskHistory({ milestoneId: 7, taskNo: 3 });
     * ```
     */
    async taskHistory(input: unknown) {

        const opts = TaskHistoryOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('milestone_id', '=', opts.milestoneId)
            .where('task_no', '=', opts.taskNo)
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Relevance-status history for a single memory. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.memoryHistory({ memoryId: 42 });
     * ```
     */
    async memoryHistory(input: unknown) {

        const opts = MemoryHistoryOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('memory_id', '=', opts.memoryId)
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Relevance-status history for a single note. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.noteHistory({ noteId: 11 });
     * ```
     */
    async noteHistory(input: unknown) {

        const opts = NoteHistoryOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('note_id', '=', opts.noteId)
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Relevance-status history for a single artifact. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.artifactHistory({ artifactId: 5 });
     * ```
     */
    async artifactHistory(input: unknown) {

        const opts = ArtifactHistoryOpts.parse(input);

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('artifact_id', '=', opts.artifactId)
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * All transitions an agent has made. When `sinceDays` is supplied,
     * the window is restricted to the last N days; otherwise the agent's
     * full history is returned. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const log = await db.audit.qry.agentHistory({ agentId: 1, sinceDays: 7 });
     * ```
     */
    async agentHistory(input: unknown) {

        const opts = AgentHistoryOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('agent_id', '=', opts.agentId);

        if (opts.sinceDays !== undefined) {

            // sql.raw is safe here: opts.sinceDays was validated by Zod as a
            // positive integer, so it cannot carry SQL-injection payloads.
            q = q.where(
                'occurred_at',
                '>',
                sql<Date>`NOW() - INTERVAL '${sql.raw(String(opts.sinceDays))} days'`,
            );

        }

        return q
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Per-agent activity rollup from `vw_Agent_Activity` — counts of
     * memories / notes / artifacts / milestones / tasks / tags created,
     * total transitions made, memories superseded, and the timestamp of
     * the most recent action.
     *
     * Returns `undefined` for an agent with no activity yet.
     *
     * @example
     * ```typescript
     * const stats = await db.audit.qry.agentActivity(1);
     * ```
     */
    async agentActivity(agentId: number) {

        return this.ctx.kysely
            .selectFrom('vw_Agent_Activity')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirst();

    }

    /**
     * Paginated `vw_Agent_Activity` — useful for "team dashboard" views
     * that want every agent's rollup at once. Ordered alphabetically by
     * agent name so the page is stable across calls.
     *
     * @example
     * ```typescript
     * const page = await db.audit.qry.agentActivityAll({ limit: 25 });
     * ```
     */
    async agentActivityAll(opts: { limit?: number; offset?: number }) {

        const limit  = Math.min(opts.limit ?? 50, 500);
        const offset = opts.offset ?? 0;

        return this.ctx.kysely
            .selectFrom('vw_Agent_Activity')
            .selectAll()
            .orderBy('name', 'asc')
            .limit(limit)
            .offset(offset)
            .execute();

    }

    /**
     * Cross-entity recent-activity stream from `vw_Recent_Activity`.
     * Ordered newest-first; the optional `entityType` narrows the feed
     * to a single entity kind.
     *
     * @example
     * ```typescript
     * const feed = await db.audit.qry.recentActivity({ limit: 25 });
     * const just = await db.audit.qry.recentActivity({ entityType: 'memory' });
     * ```
     */
    async recentActivity(input: unknown) {

        const opts = RecentActivityOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_Recent_Activity')
            .selectAll();

        if (opts.entityType !== undefined) {

            q = q.where('entity_type', '=', opts.entityType);

        }

        return q
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

    /**
     * Soft-delete recoveries — every transition where a relevance status
     * moved away from `'deleted'`. Useful for "what got un-deleted?"
     * audits and for spotting accidental restore cascades.
     *
     * The `LIKE '%-relevance'` filter is what restricts to relevance
     * transitions (memory / note / artifact / milestone). Optional
     * `sinceDays` bounds the window. Ordered newest-first.
     *
     * @example
     * ```typescript
     * const recoveries = await db.audit.qry.recoveries({ sinceDays: 30 });
     * ```
     */
    async recoveries(input: unknown) {

        const opts = RecoveriesOpts.parse(input);

        let q = this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('state_transition_type', 'like', '%-relevance')
            .where('from_status', '=', 'deleted');

        if (opts.sinceDays !== undefined) {

            // sql.raw is safe here: opts.sinceDays was validated by Zod as a
            // positive integer, so it cannot carry SQL-injection payloads.
            q = q.where(
                'occurred_at',
                '>',
                sql<Date>`NOW() - INTERVAL '${sql.raw(String(opts.sinceDays))} days'`,
            );

        }

        return q
            .orderBy('occurred_at', 'desc')
            .limit(opts.limit)
            .offset(opts.offset)
            .execute();

    }

}
