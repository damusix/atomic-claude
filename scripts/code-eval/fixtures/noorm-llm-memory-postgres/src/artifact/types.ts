import type { Generated } from 'kysely';

/**
 * Row shape for the `Artifact` table.
 *
 * An Artifact is a tangible reference (file, link, document) elevated to its
 * own entity so it can carry provenance, relevance state, tags, and notes —
 * and be attached to multiple Milestones / Tasks via the join tables.
 *
 * `Generated<T>` marks columns the database fills in: the identity PK,
 * `relevance_status` (defaulted to `'active'` by `sp_Artifact_Create`), and
 * the two timestamp columns. Callers may omit those on Kysely inserts.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('Artifact')
 *     .selectAll()
 *     .where('artifact_id', '=', 1)
 *     .executeTakeFirst();
 * ```
 */
export interface ArtifactRow {
    artifact_id:      Generated<number>;
    relevance_status: Generated<string>;
    provenance_id:    number;
    agent_id:         number;
    title:            string;
    description:      string;
    filepath:         string;
    reason:           string;
    created_at:       Generated<Date>;
    updated_at:       Generated<Date>;
}

/**
 * Stored-procedure contract for the Artifact domain.
 *
 * Each entry is an `[ArgsType, ReturnType]` tuple. Argument keys use
 * snake_case to match the PostgreSQL parameter names (with the `p_`
 * prefix dropped). `sp_Artifact_Create` returns the new id; every
 * other proc returns nothing.
 *
 * `Delete` and `Restore` are thin wrappers over `SetRelevance` (they
 * forward to the `'deleted'` / `'active'` target status), so they
 * share the same agent-and-reason argument shape.
 *
 * @example
 * ```typescript
 * const { artifact_id } = await ctx.proc('sp_Artifact_Create', {
 *     title: 'design.md', description: '', filepath: 'docs/design.md',
 *     reason: 'initial spec', provenance_id: 1, agent_id: 1,
 * });
 * ```
 */
export interface ArtifactProcs {
    'sp_Artifact_Create': [
        {
            p_title:         string;
            p_description:   string;
            p_filepath:      string;
            p_reason:        string;
            p_provenance_id: number;
            p_agent_id:      number;
        },
        { artifact_id: number },
    ];
    'sp_Artifact_Update': [
        {
            p_artifact_id: number;
            p_title:       string;
            p_description: string;
            p_filepath:    string;
            p_reason:      string;
        },
        void,
    ];
    'sp_Artifact_SetRelevance': [
        {
            p_artifact_id:          number;
            p_new_relevance_status: string;
            p_agent_id:             number;
            p_reason:               string;
        },
        void,
    ];
    'sp_Artifact_Delete': [
        { p_artifact_id: number; p_agent_id: number; p_reason: string },
        void,
    ];
    'sp_Artifact_Restore': [
        { p_artifact_id: number; p_agent_id: number; p_reason: string },
        void,
    ];
    'sp_Artifact_Attach_Milestone': [
        { p_artifact_id: number; p_milestone_id: number },
        void,
    ];
    'sp_Artifact_Detach_Milestone': [
        { p_artifact_id: number; p_milestone_id: number },
        void,
    ];
    'sp_Artifact_Attach_Task': [
        { p_artifact_id: number; p_milestone_id: number; p_task_no: number },
        void,
    ];
    'sp_Artifact_Detach_Task': [
        { p_artifact_id: number; p_milestone_id: number; p_task_no: number },
        void,
    ];
}
