/**
 * Zod input schemas for NoteCommands.
 *
 * Caller-facing field names (camelCase) are mapped to the proc's
 * snake_case parameter shape inside commands.ts. Length limits mirror
 * the SQL DDL so we fail fast with a readable error before round-
 * tripping. The three create variants share a base shape and add the
 * subtype-specific identifier the proc needs.
 */
import { z } from 'zod';

const NoteCreateBase = z.object({
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
    provenanceId: z.number().int().nonnegative().default(0),
    agentId: z.number().int().nonnegative().default(0),
});

export const CreateProjectNoteInput = NoteCreateBase.extend({
    projectId: z.number().int().positive(),
});
export type CreateProjectNoteInput = z.infer<typeof CreateProjectNoteInput>;

export const CreateMilestoneNoteInput = NoteCreateBase.extend({
    milestoneId: z.number().int().positive(),
});
export type CreateMilestoneNoteInput = z.infer<typeof CreateMilestoneNoteInput>;

export const CreateTaskNoteInput = NoteCreateBase.extend({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
});
export type CreateTaskNoteInput = z.infer<typeof CreateTaskNoteInput>;

export const UpdateNoteInput = z.object({
    noteId: z.number().int().positive(),
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
});
export type UpdateNoteInput = z.infer<typeof UpdateNoteInput>;

export const SetNoteRelevanceInput = z.object({
    noteId: z.number().int().positive(),
    newRelevanceStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetNoteRelevanceInput = z.infer<typeof SetNoteRelevanceInput>;

export const DeleteNoteInput = z.object({
    noteId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type DeleteNoteInput = z.infer<typeof DeleteNoteInput>;

export const RestoreNoteInput = z.object({
    noteId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type RestoreNoteInput = z.infer<typeof RestoreNoteInput>;
