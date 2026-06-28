/**
 * Row + proc contracts for the Milestone domain.
 *
 * Milestone is the only elevated entity with TWO state-machine axes:
 * tracking_status (lifecycle) and relevance_status (soft-delete). Both
 * axes write to the same Milestone_StateTransition subtype, with the
 * basetype state_transition_type discriminating the axis. Project_Milestone
 * is owned here because Milestone owns the lifecycle of that join row.
 */
import type { Generated } from 'kysely';

/** Physical Milestone table row. Two status columns drive separate state machines. */
export interface MilestoneRow {
    milestone_id: Generated<number>;
    tracking_status: string;
    relevance_status: string;
    provenance_id: Generated<number>;
    agent_id: Generated<number>;
    title: Generated<string>;
    content: Generated<string>;
    reason: Generated<string>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * Subtype row joining a StateTransition to its Milestone. Accepts both
 * 'milestone-tracking' and 'milestone-relevance' state_transition_type
 * values; fn_StateTransitionIsMilestoneAxis enforces the discriminator.
 */
export interface MilestoneStateTransitionRow {
    transition_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/** Tag-to-Milestone binary fact. Composite PK (tag_id, milestone_id). */
export interface MilestoneTagRow {
    tag_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/** Project-to-Milestone binary fact. Composite PK (project_id, milestone_id). */
export interface ProjectMilestoneRow {
    project_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * vw_Milestone_Stats shape — one row per milestone with rollup counts
 * over tasks, artifacts, notes, tags, dependencies, and projects.
 * blocked_tasks counts DISTINCT tasks under this milestone with at least
 * one Task_Dependency of verb 'blocks' whose target is not 'done'.
 */
export interface MilestoneStatsView {
    milestone_id: number;
    title: string;
    content: string;
    reason: string;
    tracking_status: string;
    relevance_status: string;
    provenance_id: number;
    total_tasks: number;
    open_tasks: number;
    done_tasks: number;
    abandoned_tasks: number;
    blocked_tasks: number;
    total_artifacts: number;
    total_notes: number;
    total_tags: number;
    total_dependencies: number;
    project_count: number;
    created_at: Date;
    updated_at: Date;
}

/**
 * Stored procedure contracts owned by the Milestone domain.
 *
 * Nine procs cover create / update / two state-machine moves (SetTracking
 * + SetRelevance) / Delete-Restore wrappers / Close (orchestrated
 * done + superseded + abandon-open-tasks) / Attach-Detach Project.
 */
export interface MilestoneProcs {

    'sp_Milestone_Create': [
        {
            title: string;
            content?: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
        },
        { milestone_id: number },
    ];

    'sp_Milestone_Update': [
        {
            milestone_id: number;
            title: string;
            content?: string;
            reason?: string;
        },
        void,
    ];

    'sp_Milestone_SetTracking': [
        {
            milestone_id: number;
            new_tracking_status: string;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Milestone_SetRelevance': [
        {
            milestone_id: number;
            new_relevance_status: string;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Milestone_Delete': [
        { milestone_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Milestone_Restore': [
        { milestone_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Milestone_Close': [
        { milestone_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Milestone_Attach_Project': [
        { milestone_id: number; project_id: number },
        void,
    ];

    'sp_Milestone_Detach_Project': [
        { milestone_id: number; project_id: number },
        void,
    ];

}
