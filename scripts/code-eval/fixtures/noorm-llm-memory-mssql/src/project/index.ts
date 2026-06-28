/**
 * Public surface for the Project domain.
 *
 * Re-exports the row + proc contracts, the Zod input schemas, and the
 * command/query classes so the package facade can compose them without
 * digging into per-file paths.
 */
export { ProjectCommands } from './commands';
export { ProjectQueries } from './queries';

export type {
    ProjectRow,
    ProjectTagRow,
    ProjectProcs,
} from './types';

export {
    CreateProjectInput,
    UpdateProjectInput,
    DeleteProjectInput,
} from './schema';
