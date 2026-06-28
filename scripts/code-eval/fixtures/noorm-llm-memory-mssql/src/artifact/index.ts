/**
 * Public surface for the Artifact domain.
 *
 * Re-exports the row + proc contracts, the Zod input schemas, and the
 * command/query classes so the package facade can compose them without
 * digging into per-file paths.
 */
export { ArtifactCommands } from './commands';
export { ArtifactQueries } from './queries';

export type {
    ArtifactRow,
    ArtifactStateTransitionRow,
    ArtifactTagRow,
    MilestoneArtifactRow,
    TaskArtifactRow,
    ArtifactProcs,
} from './types';

export {
    CreateArtifactInput,
    UpdateArtifactInput,
    SetArtifactRelevanceInput,
    DeleteArtifactInput,
    RestoreArtifactInput,
    AttachArtifactToMilestoneInput,
    DetachArtifactFromMilestoneInput,
    AttachArtifactToTaskInput,
    DetachArtifactFromTaskInput,
} from './schema';
