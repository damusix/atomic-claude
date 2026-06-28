import type { Generated } from 'kysely';

/**
 * Row shape for the `StateTransition` table.
 *
 * `StateTransition` is the elevated basetype for the audit subsystem. Every
 * tracked status change (memory relevance, milestone tracking, task tracking,
 * note relevance, artifact relevance) writes one row here plus exactly one
 * row in the matching `*_StateTransition` subtype table.
 *
 * Transitions are **immutable** — once written, the row is never updated, so
 * there is no `updated_at` column. `transition_id`, `occurred_at`, and
 * `created_at` are filled by Postgres on insert and so are wrapped with
 * `Generated<T>` to let Kysely omit them.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('StateTransition')
 *     .selectAll()
 *     .where('transition_id', '=', 1)
 *     .executeTakeFirst();
 * ```
 */
export interface StateTransitionRow {
    transition_id:         Generated<number>;
    state_transition_type: string;
    agent_id:              number;
    from_status:           string;
    to_status:             string;
    reason:                string;
    occurred_at:           Generated<Date>;
    created_at:            Generated<Date>;
}
