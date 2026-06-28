import { z } from 'zod';

/**
 * Shared pagination shape used by every audit read method.
 *
 * Defaults: `limit = 50`, `offset = 0`. `limit` is hard-capped at 500 so
 * accidental "show me everything" calls can't sweep the whole audit log.
 */
const Pagination = {
    limit:  z.number().int().positive().max(500).default(50),
    offset: z.number().int().nonnegative().default(0),
};

/**
 * Input shape for `AuditQueries.milestoneHistory`.
 *
 * Filters `vw_StateTransition` to a single milestone's lifecycle. The query
 * additionally pins `task_no = 0` so only milestone-level transitions
 * (tracking + relevance changes) appear — task-level rows for the same
 * milestone are reachable via {@link TaskHistoryOpts}.
 *
 * @example
 * ```typescript
 * const args = MilestoneHistoryOpts.parse({ milestoneId: 7 });
 * // → { milestoneId: 7, limit: 50, offset: 0 }
 * ```
 */
export const MilestoneHistoryOpts = z.object({
    milestoneId: z.number().int().nonnegative(),
    ...Pagination,
});
export type MilestoneHistoryOpts = z.infer<typeof MilestoneHistoryOpts>;

/**
 * Input shape for `AuditQueries.taskHistory`.
 *
 * Targets the composite key (`milestone_id`, `task_no`) so callers see
 * tracking transitions for one specific task.
 *
 * @example
 * ```typescript
 * const args = TaskHistoryOpts.parse({ milestoneId: 7, taskNo: 3 });
 * ```
 */
export const TaskHistoryOpts = z.object({
    milestoneId: z.number().int().nonnegative(),
    taskNo:      z.number().int().nonnegative(),
    ...Pagination,
});
export type TaskHistoryOpts = z.infer<typeof TaskHistoryOpts>;

/**
 * Input shape for `AuditQueries.memoryHistory`.
 *
 * Filters `vw_StateTransition` to one memory's relevance changes.
 *
 * @example
 * ```typescript
 * const args = MemoryHistoryOpts.parse({ memoryId: 42 });
 * ```
 */
export const MemoryHistoryOpts = z.object({
    memoryId: z.number().int().nonnegative(),
    ...Pagination,
});
export type MemoryHistoryOpts = z.infer<typeof MemoryHistoryOpts>;

/**
 * Input shape for `AuditQueries.noteHistory`.
 *
 * Filters `vw_StateTransition` to one note's relevance changes.
 *
 * @example
 * ```typescript
 * const args = NoteHistoryOpts.parse({ noteId: 11 });
 * ```
 */
export const NoteHistoryOpts = z.object({
    noteId: z.number().int().nonnegative(),
    ...Pagination,
});
export type NoteHistoryOpts = z.infer<typeof NoteHistoryOpts>;

/**
 * Input shape for `AuditQueries.artifactHistory`.
 *
 * Filters `vw_StateTransition` to one artifact's relevance changes.
 *
 * @example
 * ```typescript
 * const args = ArtifactHistoryOpts.parse({ artifactId: 5 });
 * ```
 */
export const ArtifactHistoryOpts = z.object({
    artifactId: z.number().int().nonnegative(),
    ...Pagination,
});
export type ArtifactHistoryOpts = z.infer<typeof ArtifactHistoryOpts>;

/**
 * Input shape for `AuditQueries.agentHistory`.
 *
 * Filters `vw_StateTransition` to all transitions made by one agent. The
 * optional `sinceDays` further restricts to the last N days
 * (`occurred_at > NOW() - sinceDays * INTERVAL '1 day'`); omit it to see
 * the agent's full history.
 *
 * @example
 * ```typescript
 * const args = AgentHistoryOpts.parse({ agentId: 1, sinceDays: 7 });
 * ```
 */
export const AgentHistoryOpts = z.object({
    agentId:   z.number().int().nonnegative(),
    sinceDays: z.number().int().positive().optional(),
    ...Pagination,
});
export type AgentHistoryOpts = z.infer<typeof AgentHistoryOpts>;

/**
 * Input shape for `AuditQueries.recentActivity`.
 *
 * `vw_Recent_Activity` is a cross-entity stream; the optional `entityType`
 * filter narrows it to just one entity kind (e.g. only memory activity).
 *
 * @example
 * ```typescript
 * const args = RecentActivityOpts.parse({ entityType: 'memory', limit: 25 });
 * ```
 */
export const RecentActivityOpts = z.object({
    entityType: z
        .enum(['memory', 'note', 'milestone', 'task', 'artifact', 'tag', 'project'])
        .optional(),
    ...Pagination,
});
export type RecentActivityOpts = z.infer<typeof RecentActivityOpts>;

/**
 * Input shape for `AuditQueries.recoveries`.
 *
 * Surfaces transitions of any `*-relevance` type from `'deleted'` to any
 * other status — i.e. soft-delete recoveries. Optional `sinceDays` limits
 * the window the same way `AgentHistoryOpts.sinceDays` does.
 *
 * @example
 * ```typescript
 * const args = RecoveriesOpts.parse({ sinceDays: 30 });
 * ```
 */
export const RecoveriesOpts = z.object({
    sinceDays: z.number().int().positive().optional(),
    ...Pagination,
});
export type RecoveriesOpts = z.infer<typeof RecoveriesOpts>;
