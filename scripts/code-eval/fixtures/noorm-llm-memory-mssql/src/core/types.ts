/**
 * Compose the project-wide DB schema, proc/func/tvf contracts, and
 * row types from each domain's local declarations.
 *
 * Adding a new proc means editing the domain's types.ts. The composer
 * here is mechanical and rarely changes.
 *
 * Reference table row types are also colocated here because they have
 * no domain owner — they're shared lookup vocabulary.
 */
import type {
    AgentRow,
    AgentProcs,
} from '../agent/types';
import type {
    ArtifactRow,
    ArtifactStateTransitionRow,
    ArtifactTagRow,
    ArtifactProcs,
    MilestoneArtifactRow,
    TaskArtifactRow,
} from '../artifact/types';
import type {
    AuditViewRow,
    StateTransitionRow,
} from '../audit/types';
import type {
    MemoryRow,
    MemoryStateTransitionRow,
    MemoryTagRow,
    ProjectMemoryRow,
    RelatedMemoryRow,
    MemoryProcs,
    MemoryFuncs,
    MemoryView,
} from '../memory/types';
import type {
    MilestoneRow,
    MilestoneStateTransitionRow,
    MilestoneTagRow,
    ProjectMilestoneRow,
    MilestoneProcs,
    MilestoneStatsView,
} from '../milestone/types';
import type {
    NoteRow,
    NoteStateTransitionRow,
    ProjectNoteRow,
    MilestoneNoteRow,
    TaskNoteRow,
    NoteProcs,
    NoteFuncs,
    NoteView,
} from '../note/types';
import type {
    ProjectRow,
    ProjectTagRow,
    ProjectProcs,
} from '../project/types';
import type {
    TagRow,
    TagProcs,
    TagTvfs,
    TagView,
} from '../tag/types';
import type {
    TaskRow,
    TaskStateTransitionRow,
    TaskTagRow,
    TaskDependencyRow,
    TaskProcs,
    TaskFuncs,
    TaskBacklogView,
} from '../task/types';

/**
 * Reference (lookup) table row shapes. These are written by hand here
 * because the entries are short and have no per-domain home.
 */
export interface RelevanceStatusRow { relevance_status: string; }
export interface TrackingStatusRow { tracking_status: string; }
export interface NoteTypeRow { note_type: string; }
export interface MemoryDomainRow { domain: string; }
export interface MemoryCategoryRow { category: string; }
export interface DependencyVerbRow { dependency_verb: string; }
export interface MemoryRelationVerbRow {
    verb_forward: string;
    verb_backward: string;
}
export interface StateTransitionTypeRow { state_transition_type: string; }
export interface TrackingStatusAllowedRow {
    from_status: string;
    to_status: string;
}
export interface RelevanceStatusAllowedRow {
    from_status: string;
    to_status: string;
}

/** The Kysely database schema. Use bracketed identifiers for queries. */
export interface DB {

    // Reference
    RelevanceStatus: RelevanceStatusRow;
    TrackingStatus: TrackingStatusRow;
    NoteType: NoteTypeRow;
    MemoryDomain: MemoryDomainRow;
    MemoryCategory: MemoryCategoryRow;
    DependencyVerb: DependencyVerbRow;
    MemoryRelationVerb: MemoryRelationVerbRow;
    StateTransitionType: StateTransitionTypeRow;
    TrackingStatus_Allowed: TrackingStatusAllowedRow;
    RelevanceStatus_Allowed: RelevanceStatusAllowedRow;

    // Elevated
    Agent: AgentRow;
    Project: ProjectRow;
    Note: NoteRow;
    Tag: TagRow;
    Memory: MemoryRow;
    Artifact: ArtifactRow;
    Milestone: MilestoneRow;
    StateTransition: StateTransitionRow;

    // Hierarchic
    Task: TaskRow;

    // Note subtypes
    Project_Note: ProjectNoteRow;
    Milestone_Note: MilestoneNoteRow;
    Task_Note: TaskNoteRow;

    // StateTransition subtypes
    Milestone_StateTransition: MilestoneStateTransitionRow;
    Task_StateTransition: TaskStateTransitionRow;
    Memory_StateTransition: MemoryStateTransitionRow;
    Note_StateTransition: NoteStateTransitionRow;
    Artifact_StateTransition: ArtifactStateTransitionRow;

    // Binary facts (Tag joins)
    Project_Tag: ProjectTagRow;
    Memory_Tag: MemoryTagRow;
    Artifact_Tag: ArtifactTagRow;
    Milestone_Tag: MilestoneTagRow;
    Task_Tag: TaskTagRow;

    // Other binary facts
    Project_Memory: ProjectMemoryRow;
    Project_Milestone: ProjectMilestoneRow;
    Related_Memory: RelatedMemoryRow;
    Milestone_Artifact: MilestoneArtifactRow;
    Task_Artifact: TaskArtifactRow;
    Task_Dependency: TaskDependencyRow;

    // Views (read-only — Kysely treats them as selectable tables)
    vw_Tag: TagView;
    vw_Memory: MemoryView;
    vw_Note: NoteView;
    vw_Milestone_Stats: MilestoneStatsView;
    vw_Task_Backlog: TaskBacklogView;
    vw_StateTransition: AuditViewRow;
    // Other views are accessed by name when needed; their shapes can be
    // declared inline at call sites if narrower projections are useful.

}

/** Stored procedure contracts, keyed by proc name. */
export type Procs =
    & AgentProcs
    & ProjectProcs
    & MemoryProcs
    & NoteProcs
    & TagProcs
    & ArtifactProcs
    & MilestoneProcs
    & TaskProcs;

/** Database (scalar) function contracts. */
export type Funcs =
    & MemoryFuncs
    & NoteFuncs
    & TaskFuncs;

/** Table-valued function contracts. */
export type Tvfs = TagTvfs;
