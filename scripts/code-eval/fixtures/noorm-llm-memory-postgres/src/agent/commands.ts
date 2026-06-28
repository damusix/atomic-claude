import { Repo } from '../core/repo';

import {
    CreateAgentInput,
    DeleteAgentInput,
    UpdateAgentInput,
} from './schema';

/**
 * Mutation surface for the `Agent` domain.
 *
 * One method per `sp_Agent_*` stored procedure, in the same order they
 * appear in the schema artifact. Each method:
 *
 *   1. Parses untyped input through the matching Zod schema.
 *   2. Maps camelCase fields onto the proc's snake_case args.
 *   3. Awaits the proc and unwraps the return value where useful
 *      (e.g. `create` returns a bare `number` rather than `{ agent_id }`).
 *
 * Sentinel-id guards (rejecting `agent_id = 0` on update / delete) live
 * in SQL — this layer just forwards the call.
 *
 * @example
 * ```typescript
 * const agentId = await db.agent.cmd.create({ name: 'claude-opus-4-7' });
 * await db.agent.cmd.update({ agentId, name: 'claude-opus', description: 'v4.7' });
 * await db.agent.cmd.softDelete({ agentId });
 * ```
 */
export class AgentCommands extends Repo {

    /**
     * Register a new agent and return its id.
     *
     * @example
     * ```typescript
     * const agentId = await db.agent.cmd.create({
     *     name: 'claude-opus-4-7',
     *     description: 'authoring assistant',
     * });
     * ```
     */
    async create(input: unknown): Promise<number> {

        const args = CreateAgentInput.parse(input);

        const [row] = await this.ctx.proc('sp_Agent_Create', {
            p_name:        args.name,
            p_description: args.description,
        });

        if (!row) throw new Error('sp_Agent_Create returned no rows');

        return row.agent_id;

    }

    /**
     * Update an agent's display fields. Rejected by SQL when `agentId = 0`.
     *
     * @example
     * ```typescript
     * await db.agent.cmd.update({
     *     agentId: 7,
     *     name: 'claude-opus',
     *     description: 'renamed for v4.7 release',
     * });
     * ```
     */
    async update(input: unknown): Promise<void> {

        const args = UpdateAgentInput.parse(input);

        await this.ctx.proc('sp_Agent_Update', {
            p_agent_id:    args.agentId,
            p_name:        args.name,
            p_description: args.description,
        });

    }

    /**
     * Hard-delete an agent. The proc reassigns any `agent_id`-referencing
     * rows to the `none` sentinel (id 0) before removing the row, so this
     * never orphans a memory / note / etc. Rejected by SQL when `agentId = 0`.
     *
     * Named `softDelete` (not `delete`) to match the project-wide convention
     * and avoid colliding with the JS `delete` keyword as a method name.
     *
     * @example
     * ```typescript
     * await db.agent.cmd.softDelete({ agentId: 7 });
     * ```
     */
    async softDelete(input: unknown): Promise<void> {

        const args = DeleteAgentInput.parse(input);

        await this.ctx.proc('sp_Agent_Delete', {
            p_agent_id: args.agentId,
        });

    }

}
