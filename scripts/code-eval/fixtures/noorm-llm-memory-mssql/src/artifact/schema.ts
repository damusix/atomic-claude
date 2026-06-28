/**
 * Zod input schemas for ArtifactCommands.
 *
 * Caller-facing field names use camelCase; commands.ts maps them to the
 * proc's snake_case parameter shape. Length limits mirror the SQL column
 * definitions so we fail fast with a readable error before round-tripping.
 *
 * relevanceStatus is a plain string — the proc and CHECK constraints
 * validate against the reference table and the allowed-transition matrix.
 */
import { z } from 'zod';

export const CreateArtifactInput = z.object({
    title: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
    filepath: z.string().max(255).default(''),
    reason: z.string().max(255).default(''),
    provenanceId: z.number().int().nonnegative().default(0),
    agentId: z.number().int().nonnegative().default(0),
});
export type CreateArtifactInput = z.infer<typeof CreateArtifactInput>;

export const UpdateArtifactInput = z.object({
    artifactId: z.number().int().positive(),
    title: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
    filepath: z.string().max(255).default(''),
    reason: z.string().max(255).default(''),
});
export type UpdateArtifactInput = z.infer<typeof UpdateArtifactInput>;

export const SetArtifactRelevanceInput = z.object({
    artifactId: z.number().int().positive(),
    newRelevanceStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetArtifactRelevanceInput = z.infer<typeof SetArtifactRelevanceInput>;

export const DeleteArtifactInput = z.object({
    artifactId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type DeleteArtifactInput = z.infer<typeof DeleteArtifactInput>;

export const RestoreArtifactInput = z.object({
    artifactId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type RestoreArtifactInput = z.infer<typeof RestoreArtifactInput>;

export const AttachArtifactToMilestoneInput = z.object({
    artifactId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type AttachArtifactToMilestoneInput = z.infer<typeof AttachArtifactToMilestoneInput>;

export const DetachArtifactFromMilestoneInput = z.object({
    artifactId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type DetachArtifactFromMilestoneInput = z.infer<typeof DetachArtifactFromMilestoneInput>;

export const AttachArtifactToTaskInput = z.object({
    artifactId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
});
export type AttachArtifactToTaskInput = z.infer<typeof AttachArtifactToTaskInput>;

export const DetachArtifactFromTaskInput = z.object({
    artifactId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
});
export type DetachArtifactFromTaskInput = z.infer<typeof DetachArtifactFromTaskInput>;
