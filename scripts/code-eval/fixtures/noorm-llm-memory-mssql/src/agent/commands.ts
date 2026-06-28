/**
 * DML for the Agent domain.
 *
 * Each method validates input with a Zod schema, then dispatches to the
 * matching stored procedure. Sentinel-id protection (Agent(0)) lives in
 * the proc — we let RAISERROR propagate as a tedious error.
 *
 * @example
 * const agents = new AgentCommands(ctx);
 * const { agent_id } = await agents.create({ name: 'Claude' });
 */
import { Repo } from '../core/repo';

import {
    CreateAgentInput,
    DeleteAgentInput,
    UpdateAgentInput,
} from './schema';

export class AgentCommands extends Repo {

    /** Insert a new Agent and return its IDENTITY id. */
    async create(input: unknown): Promise<{ agent_id: number }> {

        const parsed = CreateAgentInput.parse(input);

        const rows = await this.ctx.proc('sp_Agent_Create', {
            name: parsed.name,
            description: parsed.description,
        });

        const row = rows[0];
        if (!row) throw new Error('sp_Agent_Create returned no rows.');

        return row;

    }

    /** Update name/description for an existing Agent (sentinel 0 rejected by proc). */
    async update(input: unknown) {

        const parsed = UpdateAgentInput.parse(input);

        return this.ctx.proc('sp_Agent_Update', {
            agent_id: parsed.agentId,
            name: parsed.name,
            description: parsed.description,
        });

    }

    /** Reassign every dependent row to Agent(0), then hard-delete the Agent. */
    async delete(input: unknown) {

        const parsed = DeleteAgentInput.parse(input);

        return this.ctx.proc('sp_Agent_Delete', {
            agent_id: parsed.agentId,
        });

    }

}
