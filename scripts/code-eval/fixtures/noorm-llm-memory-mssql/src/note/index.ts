/**
 * Public surface for the Note domain.
 *
 * Re-exports the row + proc/func contracts, the Zod input schemas, and
 * the command/query classes so the package facade can compose them
 * without digging into per-file paths.
 */
export { NoteCommands } from './commands';
export { NoteQueries } from './queries';

export type {
    NoteRow,
    NoteStateTransitionRow,
    ProjectNoteRow,
    MilestoneNoteRow,
    TaskNoteRow,
    NoteView,
    NoteProcs,
    NoteFuncs,
} from './types';

export {
    CreateProjectNoteInput,
    CreateMilestoneNoteInput,
    CreateTaskNoteInput,
    UpdateNoteInput,
    SetNoteRelevanceInput,
    DeleteNoteInput,
    RestoreNoteInput,
} from './schema';
