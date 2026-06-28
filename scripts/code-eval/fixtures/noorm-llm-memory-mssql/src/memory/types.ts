/**
 * Row + proc/func contracts for the Memory domain.
 *
 * Memory is the central knowledge entity. It owns four binary facts
 * (Memory_Tag, Project_Memory, Related_Memory) and one subtype audit
 * row (Memory_StateTransition) tied to the relevance state machine.
 * The vw_Memory view enriches the base row with a computed confidence
 * column derived from the four was_* booleans.
 */
import type { Generated } from 'kysely';

import type { TvpValue } from '@noormdev/sdk';

/**
 * Physical Memory table row. memory_id is IDENTITY; the four was_*
 * columns are BIT NOT NULL with a server default of 0 (Kysely surfaces
 * BIT as boolean), so they're Generated to keep INSERT calls optional.
 */
export interface MemoryRow {
    memory_id: Generated<number>;
    domain: string;
    category: string;
    relevance_status: string;
    provenance_id: Generated<number>;
    agent_id: Generated<number>;
    content: Generated<string>;
    reason: Generated<string>;
    was_inferred: Generated<boolean>;
    was_observed: Generated<boolean>;
    was_evidenced: Generated<boolean>;
    was_user_provided: Generated<boolean>;
    last_accessed_at: Generated<Date>;
    access_count: Generated<number>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * Subtype row joining a StateTransition (state_transition_type =
 * 'memory-relevance') to its Memory. The discriminator is enforced by
 * a CHECK constraint that calls fn_StateTransitionIsOfType.
 */
export interface MemoryStateTransitionRow {
    transition_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/** Tag-to-Memory binary fact. Composite PK (tag_id, memory_id). */
export interface MemoryTagRow {
    tag_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/** Project-to-Memory binary fact. Composite PK (project_id, memory_id). */
export interface ProjectMemoryRow {
    project_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/**
 * Directed Memory-to-Memory relation. Stored one row per direction
 * with verb_forward; vw_Related_Memory exposes the symmetric view by
 * unioning the inverted side with verb_backward.
 */
export interface RelatedMemoryRow {
    memory_id: number;
    related_memory_id: number;
    relation_verb: string;
    reason: Generated<string>;
    created_at: Generated<Date>;
}

/**
 * vw_Memory shape — base Memory columns plus a computed confidence
 * (count of true was_* booleans, 0-4) from fn_MemoryConfidence.
 */
export interface MemoryView {
    memory_id: number;
    content: string;
    reason: string;
    domain: string;
    category: string;
    relevance_status: string;
    provenance_id: number;
    agent_id: number;
    was_inferred: boolean;
    was_observed: boolean;
    was_evidenced: boolean;
    was_user_provided: boolean;
    confidence: number;
    last_accessed_at: Date;
    access_count: number;
    created_at: Date;
    updated_at: Date;
}

/**
 * Stored procedure contracts owned by the Memory domain.
 *
 * Eleven base procs cover create / update / state-machine moves
 * (SetRelevance + Delete/Restore wrappers) / Touch / Relate-Unrelate /
 * Consolidate / Attach-Detach Project. The bulk Touch variant takes a
 * MemoryIdSet TVP for batched read-side signals.
 */
export interface MemoryProcs {

    'sp_Memory_Create': [
        {
            content: string;
            domain: string;
            category: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
            was_inferred?: boolean;
            was_observed?: boolean;
            was_evidenced?: boolean;
            was_user_provided?: boolean;
        },
        { memory_id: number },
    ];

    'sp_Memory_Update': [
        {
            memory_id: number;
            content: string;
            domain: string;
            category: string;
            reason?: string;
            was_inferred?: boolean;
            was_observed?: boolean;
            was_evidenced?: boolean;
            was_user_provided?: boolean;
        },
        void,
    ];

    'sp_Memory_SetRelevance': [
        {
            memory_id: number;
            new_relevance_status: string;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Memory_Delete': [
        { memory_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Memory_Restore': [
        { memory_id: number; agent_id: number; reason?: string },
        void,
    ];

    'sp_Memory_Touch': [
        { memory_id: number; agent_id?: number },
        void,
    ];

    'sp_Memory_Relate': [
        {
            memory_id: number;
            related_memory_id: number;
            relation_verb: string;
            reason?: string;
        },
        void,
    ];

    'sp_Memory_Unrelate': [
        { memory_id: number; related_memory_id: number },
        void,
    ];

    'sp_Memory_Consolidate': [
        {
            canonical_memory_id: number;
            duplicate_memory_id: number;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Memory_Attach_Project': [
        { memory_id: number; project_id: number },
        void,
    ];

    'sp_Memory_Detach_Project': [
        { memory_id: number; project_id: number },
        void,
    ];

    'sp_Memory_Bulk_Touch': [
        { MemoryIds: TvpValue },
        void,
    ];

}

/**
 * Scalar function contracts owned by the Memory domain.
 *
 * fn_MemoryConfidence returns 0-4 (count of true was_* booleans).
 * fn_MemoryRank returns a composite retrieval score (0.0-1.0) that
 * folds confidence, recency decay, and a relevance-status weight.
 */
export interface MemoryFuncs {

    'fn_MemoryConfidence': [
        { memory_id: number },
        { confidence: number },
    ];

    'fn_MemoryRank': [
        { memory_id: number },
        { rank: number },
    ];

}
