/**
 * Public surface for the Tag domain.
 *
 * Re-exports the row + proc/tvf contracts, the Zod input schemas, and
 * the command/query classes so the package facade can compose them
 * without digging into per-file paths.
 */
export { TagCommands } from './commands';
export { TagQueries } from './queries';

export type {
    TagRow,
    TagView,
    TagProcs,
    TagTvfs,
} from './types';

export {
    CreateTagInput,
    UpdateTagInput,
    DeleteTagInput,
    MergeTagInput,
    AttachTagToProjectInput,
    DetachTagFromProjectInput,
    AttachTagToMemoryInput,
    DetachTagFromMemoryInput,
    AttachTagToArtifactInput,
    DetachTagFromArtifactInput,
    AttachTagToMilestoneInput,
    DetachTagFromMilestoneInput,
    AttachTagToTaskInput,
    DetachTagFromTaskInput,
    BulkAttachMemoryInput,
    FilterMemoriesByTagsInput,
} from './schema';
