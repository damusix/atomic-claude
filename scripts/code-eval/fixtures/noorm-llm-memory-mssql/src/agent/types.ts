/**
 * Row + proc contracts for the Agent domain.
 *
 * Agent is a top-level provenance entity: every elevated row in the schema
 * carries an agent_id pointing here. Sentinel Agent(0) represents "system"
 * and is the FK fallback when an agent is deleted.
 */
import type { Generated } from 'kysely';

/** Physical Agent table row. agent_id 0 is the protected sentinel. */
export interface AgentRow {
    agent_id: Generated<number>;
    name: string;
    description: string;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/** Stored procedure contracts owned by the Agent domain. */
export interface AgentProcs {

    'sp_Agent_Create': [
        { name: string; description?: string },
        { agent_id: number },
    ];

    'sp_Agent_Update': [
        { agent_id: number; name: string; description?: string },
        void,
    ];

    'sp_Agent_Delete': [
        { agent_id: number },
        void,
    ];

}
