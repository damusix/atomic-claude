/**
 * Zod input schemas for AgentCommands.
 *
 * Caller-facing field names (camelCase) are mapped to the proc's snake_case
 * parameter shape inside commands.ts. Length limits mirror the SQL column
 * definitions so we fail fast with a readable error before round-tripping.
 */
import { z } from 'zod';

export const CreateAgentInput = z.object({
    name: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
});
export type CreateAgentInput = z.infer<typeof CreateAgentInput>;

export const UpdateAgentInput = z.object({
    agentId: z.number().int().positive(),
    name: z.string().min(1).max(255),
    description: z.string().max(255).default(''),
});
export type UpdateAgentInput = z.infer<typeof UpdateAgentInput>;

export const DeleteAgentInput = z.object({
    agentId: z.number().int().positive(),
});
export type DeleteAgentInput = z.infer<typeof DeleteAgentInput>;
