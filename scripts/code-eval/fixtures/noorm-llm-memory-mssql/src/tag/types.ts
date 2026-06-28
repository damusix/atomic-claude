/**
 * Row + proc/tvf contracts for the Tag domain.
 *
 * Tag is an inclusive subtype: a single Tag may attach to projects,
 * memories, artifacts, milestones, and tasks via five *_Tag join
 * tables. vw_Tag UNIONs across all five so callers can browse the
 * full attachment set with a single query. The bulk attach proc and
 * the tvf_FilterMemoriesByTags TVF both take TVPs declared in
 * sql/00_types/.
 */
import type { Generated } from 'kysely';

import type { TvpValue } from '@noormdev/sdk';

/**
 * Physical Tag table row. tag_id is IDENTITY. The `color` column is a
 * short visual hint for the LLM-facing UI; it defaults to '' so it is
 * Generated (insert-optional, always returned on select).
 */
export interface TagRow {
    tag_id: Generated<number>;
    provenance_id: Generated<number>;
    agent_id: Generated<number>;
    name: Generated<string>;
    description: Generated<string>;
    reason: Generated<string>;
    color: Generated<string>;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * vw_Tag shape — one row per (tag, attached entity) pair. Tags with
 * zero attachments do not appear. Unused entity columns are 0; the
 * relation_type discriminator picks the column that's populated.
 */
export interface TagView {
    tag_id: number;
    name: string;
    description: string;
    reason: string;
    provenance_id: number;
    color: string;
    relation_type: string;
    project_id: number;
    memory_id: number;
    artifact_id: number;
    milestone_id: number;
    task_no: number;
    created_at: Date;
}

/**
 * Stored procedure contracts owned by the Tag domain.
 *
 * 14 procs: 3 CRUD + Merge + 5 idempotent Attach pairs + 5 Detach
 * pairs, plus the bulk Attach_Memory variant that takes a TVP.
 */
export interface TagProcs {

    'sp_Tag_Create': [
        {
            name: string;
            description?: string;
            reason?: string;
            provenance_id?: number;
            agent_id?: number;
        },
        { tag_id: number },
    ];

    'sp_Tag_Update': [
        {
            tag_id: number;
            name: string;
            description?: string;
            reason?: string;
        },
        void,
    ];

    'sp_Tag_Delete': [
        { tag_id: number },
        void,
    ];

    'sp_Tag_Merge': [
        {
            source_tag_id: number;
            target_tag_id: number;
            agent_id: number;
            reason?: string;
        },
        void,
    ];

    'sp_Tag_Attach_Project': [
        { tag_id: number; project_id: number },
        void,
    ];

    'sp_Tag_Detach_Project': [
        { tag_id: number; project_id: number },
        void,
    ];

    'sp_Tag_Attach_Memory': [
        { tag_id: number; memory_id: number },
        void,
    ];

    'sp_Tag_Detach_Memory': [
        { tag_id: number; memory_id: number },
        void,
    ];

    'sp_Tag_Attach_Artifact': [
        { tag_id: number; artifact_id: number },
        void,
    ];

    'sp_Tag_Detach_Artifact': [
        { tag_id: number; artifact_id: number },
        void,
    ];

    'sp_Tag_Attach_Milestone': [
        { tag_id: number; milestone_id: number },
        void,
    ];

    'sp_Tag_Detach_Milestone': [
        { tag_id: number; milestone_id: number },
        void,
    ];

    'sp_Tag_Attach_Task': [
        { tag_id: number; milestone_id: number; task_no: number },
        void,
    ];

    'sp_Tag_Detach_Task': [
        { tag_id: number; milestone_id: number; task_no: number },
        void,
    ];

    'sp_Tag_Bulk_Attach_Memory': [
        { Pairs: TvpValue },
        void,
    ];

}

/**
 * Table-valued function contracts owned by the Tag domain.
 *
 * tvf_FilterMemoriesByTags returns memories that carry every requested
 * tag (relational division). The `rank` column is fn_MemoryRank applied
 * row-wise so callers can ORDER BY rank DESC for ranked retrieval.
 */
export interface TagTvfs {

    'tvf_FilterMemoriesByTags': [
        { TagIds: TvpValue },
        { memory_id: number; content: string; rank: number },
    ];

}
