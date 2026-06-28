/**
 * Zod input schemas for TagCommands.
 *
 * Caller-facing field names (camelCase) are mapped to the proc's
 * snake_case parameter shape inside commands.ts. Length limits mirror
 * the SQL DDL. Bulk variants accept arrays of caller-friendly objects
 * that commands.ts converts into TVPs at dispatch time.
 */
import { z } from 'zod';

export const CreateTagInput = z.object({
    name: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
    reason: z.string().max(255).default(''),
    provenanceId: z.number().int().nonnegative().default(0),
    agentId: z.number().int().nonnegative().default(0),
});
export type CreateTagInput = z.infer<typeof CreateTagInput>;

export const UpdateTagInput = z.object({
    tagId: z.number().int().positive(),
    name: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
    reason: z.string().max(255).default(''),
});
export type UpdateTagInput = z.infer<typeof UpdateTagInput>;

export const DeleteTagInput = z.object({
    tagId: z.number().int().positive(),
});
export type DeleteTagInput = z.infer<typeof DeleteTagInput>;

export const MergeTagInput = z.object({
    sourceTagId: z.number().int().positive(),
    targetTagId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type MergeTagInput = z.infer<typeof MergeTagInput>;

export const AttachTagToProjectInput = z.object({
    tagId: z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type AttachTagToProjectInput = z.infer<typeof AttachTagToProjectInput>;

export const DetachTagFromProjectInput = AttachTagToProjectInput;
export type DetachTagFromProjectInput = z.infer<typeof DetachTagFromProjectInput>;

export const AttachTagToMemoryInput = z.object({
    tagId: z.number().int().positive(),
    memoryId: z.number().int().positive(),
});
export type AttachTagToMemoryInput = z.infer<typeof AttachTagToMemoryInput>;

export const DetachTagFromMemoryInput = AttachTagToMemoryInput;
export type DetachTagFromMemoryInput = z.infer<typeof DetachTagFromMemoryInput>;

export const AttachTagToArtifactInput = z.object({
    tagId: z.number().int().positive(),
    artifactId: z.number().int().positive(),
});
export type AttachTagToArtifactInput = z.infer<typeof AttachTagToArtifactInput>;

export const DetachTagFromArtifactInput = AttachTagToArtifactInput;
export type DetachTagFromArtifactInput = z.infer<typeof DetachTagFromArtifactInput>;

export const AttachTagToMilestoneInput = z.object({
    tagId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
});
export type AttachTagToMilestoneInput = z.infer<typeof AttachTagToMilestoneInput>;

export const DetachTagFromMilestoneInput = AttachTagToMilestoneInput;
export type DetachTagFromMilestoneInput = z.infer<typeof DetachTagFromMilestoneInput>;

export const AttachTagToTaskInput = z.object({
    tagId: z.number().int().positive(),
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
});
export type AttachTagToTaskInput = z.infer<typeof AttachTagToTaskInput>;

export const DetachTagFromTaskInput = AttachTagToTaskInput;
export type DetachTagFromTaskInput = z.infer<typeof DetachTagFromTaskInput>;

export const BulkAttachMemoryInput = z.object({
    pairs: z.array(z.object({
        tagId: z.number().int().positive(),
        memoryId: z.number().int().positive(),
    })).min(1),
});
export type BulkAttachMemoryInput = z.infer<typeof BulkAttachMemoryInput>;

export const FilterMemoriesByTagsInput = z.object({
    tagIds: z.array(z.number().int().positive()).min(1),
});
export type FilterMemoriesByTagsInput = z.infer<typeof FilterMemoriesByTagsInput>;
