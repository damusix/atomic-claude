import { z } from 'zod';

/**
 * Input shape for `AgentCommands.create`.
 *
 * `description` defaults to `''` so callers can register an agent with just
 * a name and add prose later — matches the SQL column default and keeps
 * the common "register me as agent X" call site terse.
 *
 * @example
 * ```typescript
 * const args = CreateAgentInput.parse({ name: 'claude-opus-4-7' });
 * // → { name: 'claude-opus-4-7', description: '' }
 * ```
 */
export const CreateAgentInput = z.object({
    name:        z.string().min(1),
    description: z.string().default(''),
});
export type CreateAgentInput = z.infer<typeof CreateAgentInput>;

/**
 * Input shape for `AgentCommands.update`.
 *
 * `agentId` is non-negative because id `0` is the `none` sentinel — the SQL
 * proc rejects updates against it, but we still accept it at the boundary
 * so the SQL-side error message is the one the caller sees.
 *
 * @example
 * ```typescript
 * await db.agent.cmd.update({ agentId: 7, name: 'claude-opus', description: '' });
 * ```
 */
export const UpdateAgentInput = z.object({
    agentId:     z.number().int().nonnegative(),
    name:        z.string(),
    description: z.string(),
});
export type UpdateAgentInput = z.infer<typeof UpdateAgentInput>;

/**
 * Input shape for `AgentCommands.softDelete`.
 *
 * Although `sp_Agent_Delete` is a hard delete (with reassignment of any
 * `agent_id`-referencing rows to the sentinel), the wrapper is still named
 * `softDelete` to match the project-wide convention and avoid the JS
 * `delete` keyword as a method name.
 *
 * @example
 * ```typescript
 * await db.agent.cmd.softDelete({ agentId: 7 });
 * ```
 */
export const DeleteAgentInput = z.object({
    agentId: z.number().int().nonnegative(),
});
export type DeleteAgentInput = z.infer<typeof DeleteAgentInput>;
