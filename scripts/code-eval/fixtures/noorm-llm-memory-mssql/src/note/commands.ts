/**
 * DML for the Note domain.
 *
 * Notes are exclusive subtypes, so creation is split into one method
 * per parent kind. Each method validates with Zod, then dispatches to
 * the matching stored procedure that inserts both the Note basetype
 * and the subtype row in a single transaction. State changes route
 * through SetRelevance (or its Delete/Restore wrappers) and emit a
 * Note_StateTransition audit row.
 *
 * @example
 * const notes = new NoteCommands(ctx);
 * const { note_id } = await notes.createForProject({
 *     projectId: 1,
 *     content: 'design decision: store notes as exclusive subtypes',
 * });
 */
import { Repo } from '../core/repo';

import {
    CreateMilestoneNoteInput,
    CreateProjectNoteInput,
    CreateTaskNoteInput,
    DeleteNoteInput,
    RestoreNoteInput,
    SetNoteRelevanceInput,
    UpdateNoteInput,
} from './schema';

export class NoteCommands extends Repo {

    /** Insert a Note attached to a Project (note_type='project'). */
    async createForProject(input: unknown): Promise<{ note_id: number }> {

        const parsed = CreateProjectNoteInput.parse(input);

        const rows = await this.ctx.proc('sp_Note_Create_Project', {
            content: parsed.content,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
            project_id: parsed.projectId,
        });

        const row = rows[0];

        if (!row) {

            throw new Error('sp_Note_Create_Project returned no rows.');
        }

        return row;

    }

    /** Insert a Note attached to a Milestone (note_type='milestone'). */
    async createForMilestone(input: unknown): Promise<{ note_id: number }> {

        const parsed = CreateMilestoneNoteInput.parse(input);

        const rows = await this.ctx.proc('sp_Note_Create_Milestone', {
            content: parsed.content,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
            milestone_id: parsed.milestoneId,
        });

        const row = rows[0];

        if (!row) {

            throw new Error('sp_Note_Create_Milestone returned no rows.');
        }

        return row;

    }

    /** Insert a Note attached to a Task (note_type='task'). */
    async createForTask(input: unknown): Promise<{ note_id: number }> {

        const parsed = CreateTaskNoteInput.parse(input);

        const rows = await this.ctx.proc('sp_Note_Create_Task', {
            content: parsed.content,
            reason: parsed.reason,
            provenance_id: parsed.provenanceId,
            agent_id: parsed.agentId,
            milestone_id: parsed.milestoneId,
            task_no: parsed.taskNo,
        });

        const row = rows[0];

        if (!row) {

            throw new Error('sp_Note_Create_Task returned no rows.');
        }

        return row;

    }

    /** Update content/reason on an existing Note (note_type is immutable). */
    async update(input: unknown): Promise<void> {

        const parsed = UpdateNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Update', {
            note_id: parsed.noteId,
            content: parsed.content,
            reason: parsed.reason,
        });

    }

    /** Move a Note through the relevance state machine and audit the move. */
    async setRelevance(input: unknown): Promise<void> {

        const parsed = SetNoteRelevanceInput.parse(input);

        await this.ctx.proc('sp_Note_SetRelevance', {
            note_id: parsed.noteId,
            new_relevance_status: parsed.newRelevanceStatus,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Soft-delete: route through SetRelevance with status='deleted'. */
    async delete(input: unknown): Promise<void> {

        const parsed = DeleteNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Delete', {
            note_id: parsed.noteId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

    /** Restore a soft-deleted Note: SetRelevance with status='active'. */
    async restore(input: unknown): Promise<void> {

        const parsed = RestoreNoteInput.parse(input);

        await this.ctx.proc('sp_Note_Restore', {
            note_id: parsed.noteId,
            agent_id: parsed.agentId,
            reason: parsed.reason,
        });

    }

}
