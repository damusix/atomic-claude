import type { Generated } from 'kysely';

/**
 * Row shape for the `Task` table — a hierarchic child of `Milestone`.
 *
 * Composite PK is `(milestone_id, task_no)`. `task_no` is computed by
 * `fn_NextTaskNo(milestone_id)` inside `sp_Task_Create` and inserted
 * explicitly, so neither key is `Generated<>` — both must be supplied
 * to Kysely on insert. Only `tracking_status` (defaults to `'not-started'`)
 * and the two timestamps are filled by the proc / table defaults.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('Task')
 *     .selectAll()
 *     .where('milestone_id', '=', 7)
 *     .where('task_no', '=', 3)
 *     .executeTakeFirst();
 * ```
 */
export interface TaskRow {
    milestone_id:    number;
    task_no:         number;
    tracking_status: Generated<string>;
    agent_id:        number;
    title:           string;
    content:         string;
    reason:          string;
    created_at:      Generated<Date>;
    updated_at:      Generated<Date>;
}

/**
 * Stored-procedure contract for the `Task` domain.
 *
 * Each entry is an `[args, result]` tuple. Argument keys use snake_case to
 * match the PostgreSQL parameter names (with the `p_` prefix dropped). The
 * camelCase → snake_case mapping happens inside `TaskCommands`.
 *
 * `sp_Task_Create` is the only proc with a non-void return: PostgreSQL
 * declares it `RETURNS TABLE(milestone_id INT, task_no INT)`, so the SDK
 * surfaces both columns as a single composite row. Every other proc returns
 * nothing — state changes flow through the proc, audit rows are written
 * inline (see `sp_Task_SetTracking`), and the caller awaits a `void`.
 *
 * @example
 * ```typescript
 * const row = await ctx.proc('sp_Task_Create', {
 *     milestone_id: 7, title: 'Wire DT importer', content: '',
 *     reason: 'planning', agent_id: 1,
 * });
 * console.log(row.milestone_id, row.task_no);
 * ```
 */
export interface TaskProcs {
    'sp_Task_Create': [
        {
            p_milestone_id: number;
            p_title:        string;
            p_content:      string;
            p_reason:       string;
            p_agent_id:     number;
        },
        { milestone_id: number; task_no: number },
    ];
    'sp_Task_Update': [
        {
            p_milestone_id: number;
            p_task_no:      number;
            p_title:        string;
            p_content:      string;
            p_reason:       string;
        },
        void,
    ];
    'sp_Task_SetTracking': [
        {
            p_milestone_id:         number;
            p_task_no:              number;
            p_new_tracking_status:  string;
            p_agent_id:             number;
            p_reason:               string;
        },
        void,
    ];
    'sp_Task_Delete': [
        {
            p_milestone_id: number;
            p_task_no:      number;
            p_agent_id:     number;
            p_reason:       string;
        },
        void,
    ];
    'sp_Task_Depend': [
        {
            p_milestone_id:     number;
            p_task_no:          number;
            p_dep_milestone_id: number;
            p_dep_task_no:      number;
            p_dependency_verb:  string;
            p_reason:           string;
        },
        void,
    ];
    'sp_Task_Undepend': [
        {
            p_milestone_id:     number;
            p_task_no:          number;
            p_dep_milestone_id: number;
            p_dep_task_no:      number;
        },
        void,
    ];
}

/**
 * Scalar-function contract for the `Task` domain.
 *
 * PostgreSQL names a scalar function's output column after the function
 * itself, so callers must pass the function name as the third argument
 * to `ctx.func()` to project the value out — that alias is reflected in
 * each tuple's result-row key here.
 *
 * - `fn_NextTaskNo(milestone_id)` returns `MAX(task_no) + 1` scoped to a
 *   milestone (or `1` when the milestone has no tasks yet).
 * - `fn_TaskDependencyWouldCycle(...)` walks `Task_Dependency` recursively
 *   from the proposed dependency target and returns `true` when the
 *   originating task is reachable — i.e. the new edge would close a cycle.
 *
 * @example
 * ```typescript
 * const next = await ctx.func('fn_NextTaskNo', { milestone_id: 7 }, 'fn_NextTaskNo');
 * console.log(next.fn_NextTaskNo); // 1, 2, 3, ...
 * ```
 */
export interface TaskFuncs {
    'fn_NextTaskNo': [
        { p_milestone_id: number },
        { fn_NextTaskNo: number },
    ];
    'fn_TaskDependencyWouldCycle': [
        {
            p_milestone_id:     number;
            p_task_no:          number;
            p_dep_milestone_id: number;
            p_dep_task_no:      number;
        },
        { fn_TaskDependencyWouldCycle: boolean },
    ];
}
