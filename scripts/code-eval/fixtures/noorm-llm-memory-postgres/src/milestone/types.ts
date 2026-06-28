import type { Generated } from 'kysely';

/**
 * Row shape for the `Milestone` table.
 *
 * Milestone is an elevated entity — its own PK, its own status pair, and a
 * provenance anchor onto Project. `Generated<T>` flags the columns that the
 * database (or `sp_Milestone_Create`) fills in: the identity PK, the two
 * status columns (defaulted by the create proc to `'not-started'` /
 * `'active'`), and the two timestamps.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('Milestone')
 *     .selectAll()
 *     .where('milestone_id', '=', 1)
 *     .executeTakeFirst();
 * ```
 */
export interface MilestoneRow {
    milestone_id:     Generated<number>;
    tracking_status:  Generated<string>;
    relevance_status: Generated<string>;
    provenance_id:    number;
    agent_id:         number;
    title:            string;
    content:          string;
    reason:           string;
    created_at:       Generated<Date>;
    updated_at:       Generated<Date>;
}

/**
 * Stored-procedure contract for the Milestone domain.
 *
 * Each entry is an `[ArgsType, ReturnType]` tuple. Argument keys use
 * snake_case to match the PostgreSQL parameter names (with the `p_` prefix
 * dropped). `sp_Milestone_Create` returns the new id; the remaining procs
 * return nothing — the SetTracking / SetRelevance gates, the soft-delete
 * cascade, the close-and-abandon batch, and the project attach/detach are
 * all void on success and raise on failure.
 *
 * @example
 * ```typescript
 * const { milestone_id } = await ctx.proc('sp_Milestone_Create', {
 *     title: 'Q1 launch', content: '', reason: 'kickoff',
 *     provenance_id: 1, agent_id: 1,
 * });
 * await ctx.proc('sp_Milestone_SetTracking', {
 *     milestone_id, new_tracking_status: 'in-progress',
 *     agent_id: 1, reason: 'work started',
 * });
 * ```
 */
export interface MilestoneProcs {
    'sp_Milestone_Create': [
        {
            p_title:         string;
            p_content:       string;
            p_reason:        string;
            p_provenance_id: number;
            p_agent_id:      number;
        },
        { milestone_id: number },
    ];
    'sp_Milestone_Update': [
        {
            p_milestone_id: number;
            p_title:        string;
            p_content:      string;
            p_reason:       string;
        },
        void,
    ];
    'sp_Milestone_SetTracking': [
        {
            p_milestone_id:        number;
            p_new_tracking_status: string;
            p_agent_id:            number;
            p_reason:              string;
        },
        void,
    ];
    'sp_Milestone_SetRelevance': [
        {
            p_milestone_id:         number;
            p_new_relevance_status: string;
            p_agent_id:             number;
            p_reason:               string;
        },
        void,
    ];
    'sp_Milestone_Delete': [
        { p_milestone_id: number; p_agent_id: number; p_reason: string },
        void,
    ];
    'sp_Milestone_Restore': [
        { p_milestone_id: number; p_agent_id: number; p_reason: string },
        void,
    ];
    'sp_Milestone_Close': [
        { p_milestone_id: number; p_agent_id: number; p_reason: string },
        void,
    ];
    'sp_Milestone_Attach_Project': [
        { p_milestone_id: number; p_project_id: number },
        void,
    ];
    'sp_Milestone_Detach_Project': [
        { p_milestone_id: number; p_project_id: number },
        void,
    ];
}
