/**
 * Public surface for the Milestone domain.
 *
 * Re-exports the row + proc contracts, the Zod input schemas, and the
 * command/query classes so the package facade can compose them without
 * digging into per-file paths.
 */
export { MilestoneCommands } from './commands';
export { MilestoneQueries } from './queries';

export type {
    MilestoneRow,
    MilestoneStateTransitionRow,
    MilestoneTagRow,
    ProjectMilestoneRow,
    MilestoneStatsView,
    MilestoneProcs,
} from './types';

export {
    CreateMilestoneInput,
    UpdateMilestoneInput,
    SetMilestoneTrackingInput,
    SetMilestoneRelevanceInput,
    DeleteMilestoneInput,
    RestoreMilestoneInput,
    CloseMilestoneInput,
    AttachMilestoneToProjectInput,
    DetachMilestoneFromProjectInput,
} from './schema';
