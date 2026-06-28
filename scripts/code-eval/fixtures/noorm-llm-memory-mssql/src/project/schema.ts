/**
 * Zod input schemas for ProjectCommands.
 *
 * Length limits and defaults mirror the SQL DDL so callers see the same
 * boundaries the database enforces. agentId defaults to 0 (sentinel) when
 * omitted on create — matching the proc's parameter default.
 */
import { z } from 'zod';

export const CreateProjectInput = z.object({
    name: z.string().min(1).max(255),
    filepath: z.string().max(255).default(''),
    gitRepo: z.string().max(255).default(''),
    mainBranch: z.string().max(255).default(''),
    gitUrl: z.string().max(255).default(''),
    agentId: z.number().int().nonnegative().default(0),
});
export type CreateProjectInput = z.infer<typeof CreateProjectInput>;

export const UpdateProjectInput = z.object({
    projectId: z.number().int().positive(),
    name: z.string().min(1).max(255),
    filepath: z.string().max(255).default(''),
    gitRepo: z.string().max(255).default(''),
    mainBranch: z.string().max(255).default(''),
    gitUrl: z.string().max(255).default(''),
});
export type UpdateProjectInput = z.infer<typeof UpdateProjectInput>;

export const DeleteProjectInput = z.object({
    projectId: z.number().int().positive(),
});
export type DeleteProjectInput = z.infer<typeof DeleteProjectInput>;
