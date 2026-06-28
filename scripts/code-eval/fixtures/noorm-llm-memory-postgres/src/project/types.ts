import type { Generated } from 'kysely';

/**
 * Row shape for the `Project` table.
 *
 * A Project is the provenance anchor for almost every other entity in the
 * schema (Memory, Note, Tag, Artifact, Milestone all carry `provenance_id`).
 * `Generated<T>` marks columns the database fills in on insert (the identity
 * PK and the two timestamp columns), so callers may omit them when writing.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('Project')
 *     .selectAll()
 *     .where('project_id', '=', 1)
 *     .executeTakeFirst();
 * ```
 */
export interface ProjectRow {
    project_id:  Generated<number>;
    agent_id:    number;
    name:        string;
    filepath:    string;
    git_repo:    string;
    main_branch: string;
    git_url:     string;
    created_at:  Generated<Date>;
    updated_at:  Generated<Date>;
}

/**
 * Stored-procedure contract for the Project domain.
 *
 * Each entry is an `[ArgsType, ReturnType]` tuple. Argument keys use
 * snake_case to match the PostgreSQL parameter names (with the `p_`
 * prefix dropped). `sp_Project_Create` returns the new id; `Update`
 * and `Delete` return nothing.
 *
 * @example
 * ```typescript
 * const { project_id } = await ctx.proc('sp_Project_Create', {
 *     name: 'noorm', filepath: '/repos/noorm', git_repo: 'noormdev/noorm',
 *     main_branch: 'master', git_url: 'git@github.com:noormdev/noorm.git',
 *     agent_id: 1,
 * });
 * ```
 */
export interface ProjectProcs {
    'sp_Project_Create': [
        {
            p_name:        string;
            p_filepath:    string;
            p_git_repo:    string;
            p_main_branch: string;
            p_git_url:     string;
            p_agent_id:    number;
        },
        { project_id: number },
    ];
    'sp_Project_Update': [
        {
            p_project_id:  number;
            p_name:        string;
            p_filepath:    string;
            p_git_repo:    string;
            p_main_branch: string;
            p_git_url:     string;
        },
        void,
    ];
    'sp_Project_Delete': [
        { p_project_id: number },
        void,
    ];
}
