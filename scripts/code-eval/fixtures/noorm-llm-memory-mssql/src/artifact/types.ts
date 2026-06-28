/**
 * Row + proc contracts for the Artifact domain.
 *
 * Artifact is a lightweight reference (file/url/asset) that lives on a
 * Milestone or Task. The domain owns both join shapes (Milestone_Artifact,
 * Task_Artifact) because the artifact is the side that "lives on" the
 * other entity — the joins are part of the artifact's lifecycle, not the
 * milestone or task's. Relevance moves through the same state machine as
 * other elevated entities, audited via Artifact_StateTransition.
 */
import type { Generated } from 'kysely';

/** Physical Artifact table row. relevance_status drives the soft-delete flow. */
export interface ArtifactRow {
    artifact_id: Generated<number>;
    relevance_status: string;
    provenance_id: Generated<number>;
    agent_id: Generated<number>;
    title: Generated<string>;
    description: Generated<string>;
    filepath: Generated<string>;
    reason: Generated<string>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * Subtype row joining a StateTransition (state_transition_type =
 * 'artifact-relevance') to its Artifact. The discriminator is enforced
 * by a CHECK constraint that calls fn_StateTransitionIsOfType.
 */
export interface ArtifactStateTransitionRow {
    transition_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/** Tag-to-Artifact binary fact. Composite PK (tag_id, artifact_id). */
export interface ArtifactTagRow {
    tag_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/** Milestone-to-Artifact binary fact. Composite PK (milestone_id, artifact_id). */
export interface MilestoneArtifactRow {
    milestone_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/**
 * Task-to-Artifact binary fact. Composite PK includes the task's
 * (milestone_id, task_no) pair because Task itself uses a composite key.
 */
export interface TaskArtifactRow {
    milestone_id: number;
    task_no: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/**
 * Stored procedure contracts owned by the Artifact domain.
 *
 * Nine procs cover create / update / state-machine moves (SetRelevance +
 * Delete/Restore wrappers) / Attach-Detach Milestone / Attach-Detach Task.
 * Artifact has no tracking_status axis — only relevance.
 */
export interface ArtifactProcs {

    'sp_Artifact_Create': [
        {
            title: string;
            description?: string;
            filepath?: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
        },
        { artifact_id: number },
    ];

    'sp_Artifact_Update': [
        {
            artifact_id: number;
            title: string;
            description?: string;
            filepath?: string;
            reason?: string;
        },
        void,
    ];

    'sp_Artifact_SetRelevance': [
        {
            artifact_id: number;
            new_relevance_status: string;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Artifact_Delete': [
        { artifact_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Artifact_Restore': [
        { artifact_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Artifact_Attach_Milestone': [
        { artifact_id: number; milestone_id: number },
        void,
    ];

    'sp_Artifact_Detach_Milestone': [
        { artifact_id: number; milestone_id: number },
        void,
    ];

    'sp_Artifact_Attach_Task': [
        { artifact_id: number; milestone_id: number; task_no: number },
        void,
    ];

    'sp_Artifact_Detach_Task': [
        { artifact_id: number; milestone_id: number; task_no: number },
        void,
    ];

}
