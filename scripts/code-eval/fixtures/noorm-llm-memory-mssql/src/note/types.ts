/**
 * Row + proc/func contracts for the Note domain.
 *
 * Note is an exclusive subtype: each Note row pairs with exactly one of
 * Project_Note / Milestone_Note / Task_Note. The discriminator
 * (Note.note_type) is enforced by the per-subtype CHECK constraints that
 * call fn_NoteIsOfType. State changes flow through sp_Note_SetRelevance,
 * which writes a Note_StateTransition audit row alongside the update.
 */
import type { Generated } from 'kysely';

/**
 * Physical Note table row. note_id is IDENTITY; note_type is constrained
 * to 'project' | 'milestone' | 'task' by the FK to NoteType. We type it
 * as `string` rather than a literal union so Kysely query callsites stay
 * ergonomic — the database is the single source of truth for the set.
 */
export interface NoteRow {
    note_id: Generated<number>;
    note_type: string;
    relevance_status: string;
    provenance_id: Generated<number>;
    agent_id: Generated<number>;
    content: Generated<string>;
    reason: Generated<string>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * Subtype row joining a StateTransition (state_transition_type =
 * 'note-relevance') to its Note. The discriminator is enforced by the
 * CHECK that calls fn_StateTransitionIsOfType.
 */
export interface NoteStateTransitionRow {
    transition_id: number;
    note_id: number;
    created_at: Generated<Date>;
}

/** Project-typed Note subtype. PK is note_id; project_id is the parent. */
export interface ProjectNoteRow {
    note_id: number;
    project_id: number;
    created_at: Generated<Date>;
}

/** Milestone-typed Note subtype. PK is note_id; milestone_id is the parent. */
export interface MilestoneNoteRow {
    note_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * Task-typed Note subtype. PK is note_id; the parent Task is referenced
 * by the composite (milestone_id, task_no) tuple.
 */
export interface TaskNoteRow {
    note_id: number;
    milestone_id: number;
    task_no: number;
    created_at: Generated<Date>;
}

/**
 * vw_Note shape — base Note columns plus the resolved subtype keys.
 * project_id is 0 unless note_type='project'. milestone_id is 0 unless
 * note_type is 'milestone' or 'task'. task_no is 0 unless note_type='task'.
 */
export interface NoteView {
    note_id: number;
    note_type: string;
    content: string;
    reason: string;
    relevance_status: string;
    provenance_id: number;
    project_id: number;
    milestone_id: number;
    task_no: number;
}

/**
 * Stored procedure contracts owned by the Note domain.
 *
 * Three Create variants — one per subtype — each return the new note_id
 * via SCOPE_IDENTITY. Update mutates content/reason only (note_type and
 * subtype attachment are immutable). SetRelevance is the gated state-
 * machine transition; Delete/Restore are thin wrappers around it.
 */
export interface NoteProcs {

    'sp_Note_Create_Project': [
        {
            content: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
            project_id: number;
        },
        { note_id: number },
    ];

    'sp_Note_Create_Milestone': [
        {
            content: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
            milestone_id: number;
        },
        { note_id: number },
    ];

    'sp_Note_Create_Task': [
        {
            content: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
            milestone_id: number;
            task_no: number;
        },
        { note_id: number },
    ];

    'sp_Note_Update': [
        { note_id: number; content: string; reason?: string },
        void,
    ];

    'sp_Note_SetRelevance': [
        {
            note_id: number;
            new_relevance_status: string;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Note_Delete': [
        { note_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Note_Restore': [
        { note_id: number; agent_id: number; reason?: string },
        void,
    ];

}

/**
 * Scalar function contracts owned by the Note domain.
 *
 * fn_NoteSubtypeCount returns 0..3 (1 for valid data; 0 = orphaned;
 * >1 = exclusivity violation). fn_NoteMatchesSubtype returns 1 when
 * the note's declared note_type lines up with the subtype table that
 * actually holds its row.
 */
export interface NoteFuncs {

    'fn_NoteSubtypeCount': [
        { note_id: number },
        { count: number },
    ];

    'fn_NoteMatchesSubtype': [
        { note_id: number; note_type: string },
        { matches: boolean },
    ];

}
