/**
 * Zod input schemas for TaskCommands.
 *
 * Caller-facing field names use camelCase; commands.ts maps them to the
 * proc's snake_case parameter shape. Length limits mirror the SQL column
 * definitions so we fail fast with a readable error before round-tripping.
 *
 * tracking_status / dependency_verb are validated as non-empty strings
 * here — the proc layer cross-checks them against the reference tables
 * (and the tracking-state-machine allow list) and raises a tedious
 * error on miss.
 */
import { z } from 'zod';

export const CreateTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    title: z.string().min(1).max(255),
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
    agentId: z.number().int().nonnegative().default(0),
});
export type CreateTaskInput = z.infer<typeof CreateTaskInput>;

export const UpdateTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    title: z.string().min(1).max(255),
    content: z.string().default(''),
    reason: z.string().max(255).default(''),
});
export type UpdateTaskInput = z.infer<typeof UpdateTaskInput>;

export const SetTrackingInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    newTrackingStatus: z.string().min(1).max(32),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type SetTrackingInput = z.infer<typeof SetTrackingInput>;

export const DeleteTaskInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    agentId: z.number().int().nonnegative(),
    reason: z.string().max(255).default(''),
});
export type DeleteTaskInput = z.infer<typeof DeleteTaskInput>;

export const DependInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo: z.number().int().positive(),
    dependencyVerb: z.string().min(1).max(32),
    reason: z.string().max(255).default(''),
});
export type DependInput = z.infer<typeof DependInput>;

export const UndependInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo: z.number().int().positive(),
});
export type UndependInput = z.infer<typeof UndependInput>;

export const BulkDependInput = z.object({
    deps: z.array(z.object({
        milestoneId: z.number().int().positive(),
        taskNo: z.number().int().positive(),
        depMilestoneId: z.number().int().positive(),
        depTaskNo: z.number().int().positive(),
        dependencyVerb: z.string().min(1).max(32),
        reason: z.string().max(255).optional(),
    })).min(1),
});
export type BulkDependInput = z.infer<typeof BulkDependInput>;

export const NextTaskNoInput = z.object({
    milestoneId: z.number().int().positive(),
});
export type NextTaskNoInput = z.infer<typeof NextTaskNoInput>;

export const WouldCycleInput = z.object({
    milestoneId: z.number().int().positive(),
    taskNo: z.number().int().positive(),
    depMilestoneId: z.number().int().positive(),
    depTaskNo: z.number().int().positive(),
});
export type WouldCycleInput = z.infer<typeof WouldCycleInput>;
