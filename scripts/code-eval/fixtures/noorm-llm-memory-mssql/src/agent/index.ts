/**
 * Public surface for the Agent domain.
 *
 * Re-exports the row + proc contracts, the Zod input schemas, and the
 * command/query classes so the package facade can compose them without
 * digging into per-file paths.
 */
export { AgentCommands } from './commands';
export { AgentQueries } from './queries';

export type { AgentRow, AgentProcs } from './types';

export {
    CreateAgentInput,
    UpdateAgentInput,
    DeleteAgentInput,
} from './schema';
