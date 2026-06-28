/**
 * Zod input schemas for MemoryCommands.
 *
 * Caller-facing field names use camelCase; commands.ts maps them to the
 * proc's snake_case parameter shape. Length limits mirror the SQL column
 * definitions so we fail fast with a readable error before round-tripping.
 *
 * relevance_status / domain / category / relation_verb are validated as
 * non-empty strings here — the proc layer cross-checks them against the
 * reference tables and raises a tedious error on miss.
 */
import { z } from 'zod';

// Default to false rather than .optional() so missing flags survive Zod
// parse as `false`, not `undefined`. The SDK's named-parameter proc call
// passes `undefined` to the driver as NULL, which would violate the
// `BIT NOT NULL` columns on the Memory table.
const memoryFlags = {
    wasInferred: z.boolean().default(false),
    wasObserved: z.boolean().default(false),
    wasEvidenced: z.boolean().default(false),
    wasUserProvided: z.boolean().default(false),
};

export const CreateMemoryInput = z.object({
    content: z.string().min(1),
    domain: z.string().min(1).max(32),
    category: z.string().min(1).max(32),
    reason: z.string().max(255).default(''),
    provenanceId: z.number().int().nonnegative().default(0),
    agentId: z.number().int().nonnegative().default(0),
    ...memoryFlags,
});
export type CreateMemoryInput = z.infer<typeof CreateMemoryInput>;

export const UpdateMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    content: z.string().min(1),
    domain: z.string().min(1).max(32),
    category: z.string().min(1).max(32),
    reason: z.string().max(255).default(''),
    ...memoryFlags,
});
export type UpdateMemoryInput = z.infer<typeof UpdateMemoryInput>;

export const SetRelevanceInput = z.object({
    memoryId: z.number().int().positive(),
    newRelevanceStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetRelevanceInput = z.infer<typeof SetRelevanceInput>;

export const DeleteMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type DeleteMemoryInput = z.infer<typeof DeleteMemoryInput>;

export const RestoreMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type RestoreMemoryInput = z.infer<typeof RestoreMemoryInput>;

export const TouchMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    agentId: z.number().int().nonnegative().default(0),
});
export type TouchMemoryInput = z.infer<typeof TouchMemoryInput>;

export const RelateMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    relatedMemoryId: z.number().int().positive(),
    relationVerb: z.string().min(1).max(32),
    reason: z.string().max(255).default(''),
});
export type RelateMemoryInput = z.infer<typeof RelateMemoryInput>;

export const UnrelateMemoryInput = z.object({
    memoryId: z.number().int().positive(),
    relatedMemoryId: z.number().int().positive(),
});
export type UnrelateMemoryInput = z.infer<typeof UnrelateMemoryInput>;

export const ConsolidateMemoryInput = z.object({
    canonicalMemoryId: z.number().int().positive(),
    duplicateMemoryId: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type ConsolidateMemoryInput = z.infer<typeof ConsolidateMemoryInput>;

export const AttachProjectInput = z.object({
    memoryId: z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type AttachProjectInput = z.infer<typeof AttachProjectInput>;

export const DetachProjectInput = z.object({
    memoryId: z.number().int().positive(),
    projectId: z.number().int().positive(),
});
export type DetachProjectInput = z.infer<typeof DetachProjectInput>;

export const BulkTouchInput = z.object({
    memoryIds: z.array(z.number().int().positive()).min(1),
});
export type BulkTouchInput = z.infer<typeof BulkTouchInput>;
