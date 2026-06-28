/**
 * Zod input schemas for MilestoneCommands.
 *
 * Caller-facing field names use camelCase; commands.ts maps them to the
 * proc's snake_case parameter shape. Length limits mirror the SQL column
 * definitions so we fail fast with a readable error before round-tripping.
 *
 * tracking/relevance status fields are plain strings — the procs and the
 * fn_Is*TransitionAllowed validators check them at the database edge.
 */
import { z } from 'zod';

export const CreateMilestoneInput = z.object({
    title: z.string().min(1).max(255),
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
    provenanceId: z.number().int().nonnegative().default(0),
    agentId: z.number().int().nonnegative().default(0),
});
export type CreateMilestoneInput = z.infer<typeof CreateMilestoneInput>;

export const UpdateMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    title: z.string().min(1).max(255),
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
});
export type UpdateMilestoneInput = z.infer<typeof UpdateMilestoneInput>;

export const SetMilestoneTrackingInput = z.object({
    milestoneId: z.number().int().positive(),
    newTrackingStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetMilestoneTrackingInput = z.infer<typeof SetMilestoneTrackingInput>;

export const SetMilestoneRelevanceInput = z.object({
    milestoneId: z.number().int().positive(),
    newRelevanceStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetMilestoneRelevanceInput = z.infer<typeof SetMilestoneRelevanceInput>;

export const DeleteMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type DeleteMilestoneInput = z.infer<typeof DeleteMilestoneInput>;

export const RestoreMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type RestoreMilestoneInput = z.infer<typeof RestoreMilestoneInput>;

export const CloseMilestoneInput = z.object({
    milestoneId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type CloseMilestoneInput = z.infer<typeof CloseMilestoneInput>;

export const AttachMilestoneToProjectInput = z.object({
    milestoneId: z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type AttachMilestoneToProjectInput = z.infer<typeof AttachMilestoneToProjectInput>;

export const DetachMilestoneFromProjectInput = z.object({
    milestoneId: z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type DetachMilestoneFromProjectInput = z.infer<typeof DetachMilestoneFromProjectInput>;
