/**
 * Row + view contracts for the Audit domain.
 *
 * Audit owns the StateTransition basetype and the unified view
 * vw_StateTransition that LEFT JOINs the five subtype tables
 * (Milestone / Task / Memory / Note / Artifact). The subtype rows
 * themselves live in their owning domains; this domain only defines
 * the basetype shape and the cross-entity audit projection.
 *
 * There is no schema.ts or commands.ts here — every state transition
 * is written by the procs that drive the source state machines (e.g.
 * sp_Memory_SetRelevance, sp_Task_SetTracking). External callers must
 * never INSERT directly into StateTransition.
 */
import type { Generated } from 'kysely';

/**
 * Physical StateTransition table row. transition_id is IDENTITY;
 * agent_id defaults to the sentinel Agent(0); reason defaults to ''.
 * StateTransition is intentionally append-only — there is no
 * updated_at column because rows are immutable by design.
 */
export interface StateTransitionRow {
    transition_id: Generated<number>;
    state_transition_type: string;
    agent_id: Generated<number>;
    from_status: string;
    to_status: string;
    reason: Generated<string>;
    occurred_at: Generated<Date>;
    created_at: Generated<Date>;
}

/**
 * vw_StateTransition shape — the basetype columns plus every entity FK
 * surfaced via COALESCE across the five subtype tables. Exactly one of
 * (memory_id, note_id, artifact_id, task_no) is non-zero per row, and
 * milestone_id is non-zero for milestone-* and task-tracking rows.
 * Zero is the sentinel for "this entity does not apply to this row".
 */
export interface AuditViewRow {
    transition_id: number;
    state_transition_type: string;
    agent_id: number;
    from_status: string;
    to_status: string;
    reason: string;
    occurred_at: Date;
    created_at: Date;
    milestone_id: number;
    task_no: number;
    memory_id: number;
    note_id: number;
    artifact_id: number;
}
