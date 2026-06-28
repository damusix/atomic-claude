import type { Generated } from 'kysely';

/**
 * Row shape for the `Note` table.
 *
 * A Note is an exclusive subtype: every note row owns exactly one matching
 * row in `Project_Note`, `Milestone_Note`, or `Task_Note` (whichever matches
 * `note_type`). The base `Note` row carries the content, provenance, and
 * relevance status; the subtype row carries the parent FK.
 *
 * `note_id`, `relevance_status`, `created_at`, and `updated_at` are filled
 * by Postgres / by `sp_Note_Create_*` (which defaults `relevance_status` to
 * `'active'`) and so are wrapped with `Generated<T>` for insert ergonomics.
 *
 * @example
 * ```typescript
 * const row = await ctx.kysely
 *     .selectFrom('Note')
 *     .selectAll()
 *     .where('note_id', '=', 1)
 *     .executeTakeFirst();
 * ```
 */
export interface NoteRow {
    note_id:          Generated<number>;
    note_type:        string;
    relevance_status: Generated<string>;
    provenance_id:    number;
    agent_id:         number;
    content:          string;
    reason:           string;
    created_at:       Generated<Date>;
    updated_at:       Generated<Date>;
}

/**
 * Stored-procedure contract for the `Note` domain.
 *
 * Each entry is a `[args, result]` tuple. Argument keys use snake_case to
 * match the Postgres parameter names (with the `p_` prefix dropped); the
 * camelCase → snake_case mapping happens inside `NoteCommands`.
 *
 * The three `sp_Note_Create_*` procs each insert a `Note` row plus the
 * matching subtype row (`Project_Note` / `Milestone_Note` / `Task_Note`)
 * atomically and return the new `note_id`. `Update`, `SetRelevance`,
 * `Delete`, and `Restore` return nothing.
 *
 * Note: PostgreSQL functions declared `RETURNS INT` surface their value
 * under a column named after the function (e.g. `sp_Note_Create_Project`).
 * The return type below documents the *intent* (`{ note_id: number }`);
 * `NoteCommands` normalizes the actual SDK return shape to a plain `number`.
 *
 * @example
 * ```typescript
 * const { note_id } = await ctx.proc('sp_Note_Create_Project', {
 *     content: 'Initial spec posted.', reason: 'kickoff',
 *     provenance_id: 1, agent_id: 1, project_id: 1,
 * });
 * ```
 */
export interface NoteProcs {
    'sp_Note_Create_Project': [
        {
            p_content:       string;
            p_reason:        string;
            p_provenance_id: number;
            p_agent_id:      number;
            p_project_id:    number;
        },
        { note_id: number },
    ];
    'sp_Note_Create_Milestone': [
        {
            p_content:       string;
            p_reason:        string;
            p_provenance_id: number;
            p_agent_id:      number;
            p_milestone_id:  number;
        },
        { note_id: number },
    ];
    'sp_Note_Create_Task': [
        {
            p_content:       string;
            p_reason:        string;
            p_provenance_id: number;
            p_agent_id:      number;
            p_milestone_id:  number;
            p_task_no:       number;
        },
        { note_id: number },
    ];
    'sp_Note_Update': [
        {
            p_note_id: number;
            p_content: string;
            p_reason:  string;
        },
        void,
    ];
    'sp_Note_SetRelevance': [
        {
            p_note_id:              number;
            p_new_relevance_status: string;
            p_agent_id:             number;
            p_reason:               string;
        },
        void,
    ];
    'sp_Note_Delete': [
        {
            p_note_id:  number;
            p_agent_id: number;
            p_reason:   string;
        },
        void,
    ];
    'sp_Note_Restore': [
        {
            p_note_id:  number;
            p_agent_id: number;
            p_reason:   string;
        },
        void,
    ];
}
