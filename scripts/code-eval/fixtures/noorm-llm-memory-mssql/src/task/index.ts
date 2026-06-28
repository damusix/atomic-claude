/**
 * Public surface for the Task domain.
 *
 * Re-exports the row + proc/func contracts, the Zod input schemas, and
 * the command/query classes so the package facade can compose them
 * without digging into per-file paths.
 */
export { TaskCommands } from './commands';
export { TaskQueries } from './queries';

export type {
    TaskRow,
    TaskStateTransitionRow,
    TaskTagRow,
    TaskDependencyRow,
    TaskBacklogView,
    TaskProcs,
    TaskFuncs,
} from './types';

export {
    CreateTaskInput,
    UpdateTaskInput,
    SetTrackingInput,
    DeleteTaskInput,
    DependInput,
    UndependInput,
    BulkDependInput,
    NextTaskNoInput,
    WouldCycleInput,
} from './schema';
