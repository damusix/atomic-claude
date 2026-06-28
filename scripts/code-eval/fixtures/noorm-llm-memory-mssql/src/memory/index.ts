/**
 * Public surface for the Memory domain.
 *
 * Re-exports the row + proc/func contracts, the Zod input schemas, and
 * the command/query classes so the package facade can compose them
 * without digging into per-file paths.
 */
export { MemoryCommands } from './commands';
export { MemoryQueries } from './queries';

export type {
    MemoryRow,
    MemoryStateTransitionRow,
    MemoryTagRow,
    ProjectMemoryRow,
    RelatedMemoryRow,
    MemoryView,
    MemoryProcs,
    MemoryFuncs,
} from './types';

export {
    AttachProjectInput,
    BulkTouchInput,
    ConsolidateMemoryInput,
    CreateMemoryInput,
    DeleteMemoryInput,
    DetachProjectInput,
    RelateMemoryInput,
    RestoreMemoryInput,
    SetRelevanceInput,
    TouchMemoryInput,
    UnrelateMemoryInput,
    UpdateMemoryInput,
} from './schema';
