import type { Generated } from 'kysely';

import type { AgentRow, AgentProcs } from '../agent/types';
import type { ProjectRow, ProjectProcs } from '../project/types';
import type { MemoryRow, MemoryProcs, MemoryFuncs } from '../memory/types';
import type { NoteRow, NoteProcs } from '../note/types';
import type { TagRow, TagProcs } from '../tag/types';
import type { ArtifactRow, ArtifactProcs } from '../artifact/types';
import type { MilestoneRow, MilestoneProcs } from '../milestone/types';
import type { TaskRow, TaskProcs, TaskFuncs } from '../task/types';
import type { StateTransitionRow } from '../audit/types';

// ── Reference table rows (no FK, just narrow string keys) ───────────────────

/**
 * Allowed values for the `relevance_status` enum-like reference table.
 *
 * @example
 * ```typescript
 * const statuses = await ctx.kysely.selectFrom('RelevanceStatus').selectAll().execute();
 * ```
 */
export interface RelevanceStatusRow {
    relevance_status: string;
    created_at: Generated<Date>;
}

/**
 * Allowed values for the `tracking_status` enum-like reference table.
 */
export interface TrackingStatusRow {
    tracking_status: string;
    created_at: Generated<Date>;
}

/**
 * Allowed note types (e.g. `comment`, `decision`, `observation`).
 */
export interface NoteTypeRow {
    note_type: string;
    created_at: Generated<Date>;
}

/**
 * Allowed memory domains (top-level grouping for memories).
 */
export interface MemoryDomainRow {
    domain: string;
    created_at: Generated<Date>;
}

/**
 * Allowed memory categories (sub-grouping within a domain).
 */
export interface MemoryCategoryRow {
    category: string;
    created_at: Generated<Date>;
}

/**
 * Allowed verbs that describe how one task depends on another
 * (e.g. `blocks`, `requires`, `informs`).
 */
export interface DependencyVerbRow {
    dependency_verb: string;
    created_at: Generated<Date>;
}

/**
 * Allowed bidirectional verbs for memory-to-memory relationships.
 *
 * `verb_forward` and `verb_backward` are both natural-language phrasings
 * (e.g. `supersedes` / `superseded by`).
 */
export interface MemoryRelationVerbRow {
    verb_forward: string;
    verb_backward: string;
    created_at: Generated<Date>;
}

/**
 * Allowed kinds of state transitions recorded in the audit log.
 */
export interface StateTransitionTypeRow {
    state_transition_type: string;
    created_at: Generated<Date>;
}

/**
 * Allowed `tracking_status` transitions — pairs of (from, to) the workflow permits.
 */
export interface TrackingStatusAllowedRow {
    from_status: string;
    to_status: string;
    created_at: Generated<Date>;
}

/**
 * Allowed `relevance_status` transitions — pairs of (from, to) the workflow permits.
 */
export interface RelevanceStatusAllowedRow {
    from_status: string;
    to_status: string;
    created_at: Generated<Date>;
}

// ── Subtype rows ────────────────────────────────────────────────────────────

/**
 * Subtype: a Note attached to a Project.
 */
export interface ProjectNoteRow {
    note_id: number;
    project_id: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a Note attached to a Milestone.
 */
export interface MilestoneNoteRow {
    note_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a Note attached to a Task (composite key: milestone_id + task_no).
 */
export interface TaskNoteRow {
    note_id: number;
    milestone_id: number;
    task_no: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a StateTransition recorded against a Milestone.
 */
export interface MilestoneStateTransitionRow {
    transition_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a StateTransition recorded against a Task.
 */
export interface TaskStateTransitionRow {
    transition_id: number;
    milestone_id: number;
    task_no: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a StateTransition recorded against a Memory.
 */
export interface MemoryStateTransitionRow {
    transition_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a StateTransition recorded against a Note.
 */
export interface NoteStateTransitionRow {
    transition_id: number;
    note_id: number;
    created_at: Generated<Date>;
}

/**
 * Subtype: a StateTransition recorded against an Artifact.
 */
export interface ArtifactStateTransitionRow {
    transition_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

// ── Binary fact rows ─────────────────────────────────────────────────────────

/**
 * M:N — a Tag applied to a Project.
 */
export interface ProjectTagRow {
    tag_id: number;
    project_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Tag applied to a Memory.
 */
export interface MemoryTagRow {
    tag_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Tag applied to an Artifact.
 */
export interface ArtifactTagRow {
    tag_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Tag applied to a Milestone.
 */
export interface MilestoneTagRow {
    tag_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Tag applied to a Task.
 */
export interface TaskTagRow {
    tag_id: number;
    milestone_id: number;
    task_no: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Memory associated with a Project.
 */
export interface ProjectMemoryRow {
    project_id: number;
    memory_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — a Milestone associated with a Project.
 */
export interface ProjectMilestoneRow {
    project_id: number;
    milestone_id: number;
    created_at: Generated<Date>;
}

/**
 * Memory ↔ Memory relationship (e.g. supersession, contradiction, derivation).
 *
 * `relation_verb` references {@link MemoryRelationVerbRow.verb_forward}.
 */
export interface RelatedMemoryRow {
    memory_id: number;
    related_memory_id: number;
    relation_verb: string;
    reason: string;
    created_at: Generated<Date>;
}

/**
 * M:N — an Artifact attached to a Milestone.
 */
export interface MilestoneArtifactRow {
    milestone_id: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/**
 * M:N — an Artifact attached to a Task.
 */
export interface TaskArtifactRow {
    milestone_id: number;
    task_no: number;
    artifact_id: number;
    created_at: Generated<Date>;
}

/**
 * Task ↔ Task dependency (with verb describing the relationship).
 *
 * `dependency_verb` references {@link DependencyVerbRow.dependency_verb}.
 */
export interface TaskDependencyRow {
    milestone_id: number;
    task_no: number;
    dep_milestone_id: number;
    dep_task_no: number;
    dependency_verb: string;
    reason: string;
    created_at: Generated<Date>;
}

// ── View row shapes ─────────────────────────────────────────────────────────

/**
 * Polymorphic tag view — joins Tag with each of its possible attachment tables
 * and surfaces the discriminator via `relation_type`.
 */
export interface VwTagRow {
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
 * Polymorphic artifact view — joins Artifact with its parent (Milestone or Task).
 */
export interface VwArtifactRow {
    artifact_id: number;
    title: string;
    description: string;
    filepath: string;
    reason: string;
    relevance_status: string;
    provenance_id: number;
    relation_type: string;
    milestone_id: number;
    task_no: number;
    created_at: Date;
}

/**
 * Polymorphic note view — joins Note with its parent (Project, Milestone, or Task).
 */
export interface VwNoteRow {
    note_id: number;
    note_type: string;
    content: string;
    reason: string;
    relevance_status: string;
    provenance_id: number;
    project_id: number;
    milestone_id: number;
    task_no: number;
    agent_id: number;
    created_at: Date;
    updated_at: Date;
}

/**
 * Memory-to-memory relationship enriched with the chosen-direction verb.
 */
export interface VwRelatedMemoryRow {
    memory_id: number;
    related_memory_id: number;
    verb: string;
    reason: string;
    created_at: Date;
}

/**
 * Aggregated milestone metrics (counts of children by status, totals, etc.).
 */
export interface VwMilestoneStatsRow {
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
 * Memory enriched with computed `confidence` (via `fn_MemoryConfidence`).
 *
 * Used by both `vw_Memory` and `vw_Active_Memory` (same shape, different filters).
 */
export interface VwMemoryRow extends MemoryRow {
    confidence: number;
}

/**
 * Tasks not yet in a terminal state, with computed blocked flag.
 */
export interface VwTaskBacklogRow {
    milestone_id: number;
    task_no: number;
    title: string;
    content: string;
    tracking_status: string;
    agent_id: number;
    is_blocked: boolean;
    created_at: Date;
    updated_at: Date;
}

/**
 * Polymorphic state transition view — surfaces the target entity columns
 * via nullable FKs (only one entity column is populated per row).
 */
export interface VwStateTransitionRow {
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

/**
 * Recent activity feed — unioned across all entity creations and transitions.
 */
export interface VwRecentActivityRow {
    entity_type: string;
    entity_id: number;
    milestone_id: number;
    task_no: number;
    title_or_excerpt: string;
    agent_id: number;
    action_type: string;
    occurred_at: Date;
}

/**
 * Per-agent activity totals — counts of each entity type created plus
 * transitions made and the timestamp of the most recent action.
 */
export interface VwAgentActivityRow {
    agent_id: number;
    name: string;
    memories_created: number;
    notes_created: number;
    artifacts_created: number;
    milestones_created: number;
    tasks_created: number;
    tags_created: number;
    transitions_made: number;
    memories_superseded: number;
    last_action_at: Date;
}

// ── DB interface — the full Kysely schema map ───────────────────────────────

/**
 * Kysely schema map for the LLM-memory PostgreSQL database.
 *
 * Each key is a table or view name; the value is the row interface
 * (with `Generated<T>` columns for auto-populated values).
 */
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

    // Subtypes
    Project_Note: ProjectNoteRow;
    Milestone_Note: MilestoneNoteRow;
    Task_Note: TaskNoteRow;
    Milestone_StateTransition: MilestoneStateTransitionRow;
    Task_StateTransition: TaskStateTransitionRow;
    Memory_StateTransition: MemoryStateTransitionRow;
    Note_StateTransition: NoteStateTransitionRow;
    Artifact_StateTransition: ArtifactStateTransitionRow;

    // Binary
    Project_Tag: ProjectTagRow;
    Memory_Tag: MemoryTagRow;
    Artifact_Tag: ArtifactTagRow;
    Milestone_Tag: MilestoneTagRow;
    Task_Tag: TaskTagRow;
    Project_Memory: ProjectMemoryRow;
    Project_Milestone: ProjectMilestoneRow;
    Related_Memory: RelatedMemoryRow;
    Milestone_Artifact: MilestoneArtifactRow;
    Task_Artifact: TaskArtifactRow;
    Task_Dependency: TaskDependencyRow;

    // Views — typed rows (read-only).
    vw_Tag: VwTagRow;
    vw_Artifact: VwArtifactRow;
    vw_Note: VwNoteRow;
    vw_Related_Memory: VwRelatedMemoryRow;
    vw_Milestone_Stats: VwMilestoneStatsRow;
    vw_Memory: VwMemoryRow;
    vw_Active_Memory: VwMemoryRow;
    vw_Active_Note: NoteRow;
    vw_Active_Artifact: ArtifactRow;
    vw_Active_Milestone: MilestoneRow;
    vw_Deleted_Memory: VwMemoryRow;
    vw_Deleted_Note: NoteRow;
    vw_Deleted_Artifact: ArtifactRow;
    vw_Deleted_Milestone: MilestoneRow;
    vw_Task_Backlog: VwTaskBacklogRow;
    vw_StateTransition: VwStateTransitionRow;
    vw_Recent_Activity: VwRecentActivityRow;
    vw_Agent_Activity: VwAgentActivityRow;

}

// ── Procs / Funcs / Tvfs composition ────────────────────────────────────────

/**
 * Union of every domain's stored-procedure contributions.
 *
 * Each domain's `*Procs` interface declares one key per proc name with
 * `{ args, result }` shape; intersecting them produces the full proc map.
 */
export type Procs =
    & AgentProcs
    & ProjectProcs
    & MemoryProcs
    & NoteProcs
    & TagProcs
    & ArtifactProcs
    & MilestoneProcs
    & TaskProcs;

/**
 * Union of every domain's scalar-function contributions.
 *
 * Only `memory` and `task` declare scalar functions in this schema
 * (`fn_MemoryConfidence`, `fn_TaskIsBlocked`, etc.).
 */
export type Funcs = MemoryFuncs & TaskFuncs;

/**
 * Table-valued functions. PostgreSQL exposes table-returning routines via
 * the `Procs` map (they're invoked with `ctx.proc.<name>()` and return rows),
 * so this map is intentionally empty.
 */
export type Tvfs = Record<string, never>;
