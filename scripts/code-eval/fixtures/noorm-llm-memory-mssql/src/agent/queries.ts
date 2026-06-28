/**
 * DQL for the Agent domain.
 *
 * Read paths use Kysely directly so Agent rows can be projected with the
 * caller's preferred column set. The default helpers return everything.
 *
 * @example
 * const agents = new AgentQueries(ctx);
 * const all = await agents.list();
 * const one = await agents.findById(1);
 */
import { Repo } from '../core/repo';

export class AgentQueries extends Repo {

    /** Fetch a single Agent by id, or undefined when no row matches. */
    async findById(agentId: number) {

        return this.ctx.kysely
            .selectFrom('Agent')
            .selectAll()
            .where('agent_id', '=', agentId)
            .executeTakeFirst();

    }

    /** List every Agent ordered by id (sentinel 0 first). */
    async list() {

        return this.ctx.kysely
            .selectFrom('Agent')
            .selectAll()
            .orderBy('agent_id', 'asc')
            .execute();

    }

}
