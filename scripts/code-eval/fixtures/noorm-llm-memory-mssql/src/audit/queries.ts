/**
 * Read-only DQL for the Audit domain.
 *
 * Wraps vw_StateTransition / vw_Recent_Activity / vw_Agent_Activity
 * for the common access patterns: per-entity history, recent activity
 * stream, per-agent rollup, and milestone audit trail. Every method
 * uses Kysely directly; there are no audit procs (the source state
 * machines write StateTransition rows on the caller's behalf).
 *
 * vw_Recent_Activity and vw_Agent_Activity aren't in the Kysely DB
 * schema (their projections vary across consumers), so calls drop to
 * sql.raw with parameterised inputs for safety.
 *
 * @example
 * const audit = new AuditQueries(ctx);
 * const history = await audit.transitions({ entityType: 'memory', entityId: 42 });
 * const recent  = await audit.recentActivity(50);
 */
import { sql } from 'kysely';

import { Repo } from '../core/repo';

export type AuditEntityType =
    | 'memory'
    | 'note'
    | 'artifact'
    | 'milestone'
    | 'task';

export interface RecentActivityRow {
    entity_type: string;
    entity_id: number;
    milestone_id: number;
    task_no: number;
    title_or_excerpt: string;
    agent_id: number;
    action_type: string;
    occurred_at: Date;
}

export interface AgentActivityRow {
    agent_id: number;
    name: string;
    memories_created: number;
    notes_created: number;
    artifacts_created: number;
    milestones_created: number;
    tasks_created: number;
    tags_created: number;
    transitions_made: number;
    memories_superseded: number;
    last_action_at: Date | null;
}

export class AuditQueries extends Repo {

    /**
     * Transition history for a single entity. Filters vw_StateTransition
     * by the matching foreign-key column for the requested entityType;
     * Tasks use the composite (milestone_id, task_no) and pass entityId
     * as task_no with milestoneId supplied via the optional second arg.
     */
    async transitions(input: {
        entityType: AuditEntityType;
        entityId: number;
        milestoneId?: number;
    }) {

        const { entityType, entityId, milestoneId } = input;

        let q = this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll();

        if (entityType === 'memory') {

            q = q.where('memory_id', '=', entityId);
        }
        else if (entityType === 'note') {

            q = q.where('note_id', '=', entityId);
        }
        else if (entityType === 'artifact') {

            q = q.where('artifact_id', '=', entityId);
        }
        else if (entityType === 'milestone') {

            q = q
                .where('milestone_id', '=', entityId)
                .where('task_no', '=', 0);
        }
        else {

            // task — entityId is task_no, milestoneId is required
            if (milestoneId === undefined) {

                throw new Error('milestoneId is required when entityType is "task".');
            }

            q = q
                .where('milestone_id', '=', milestoneId)
                .where('task_no', '=', entityId);
        }

        return q
            .orderBy('occurred_at', 'desc')
            .orderBy('transition_id', 'desc')
            .execute();

    }

    /** Cross-entity activity stream from vw_Recent_Activity, newest first. */
    async recentActivity(limit = 100): Promise<RecentActivityRow[]> {

        const safeLimit = Math.max(1, Math.min(Math.floor(limit), 10_000));

        const result = await sql<RecentActivityRow>`
            SELECT TOP (${sql.raw(String(safeLimit))})
                [entity_type], [entity_id], [milestone_id], [task_no],
                [title_or_excerpt], [agent_id], [action_type], [occurred_at]
            FROM [dbo].[vw_Recent_Activity]
            ORDER BY [occurred_at] DESC
        `.execute(this.ctx.kysely);

        return result.rows;

    }

    /** Per-agent rollup row from vw_Agent_Activity, or undefined when no agent matches. */
    async agentActivity(agentId: number): Promise<AgentActivityRow | undefined> {

        const result = await sql<AgentActivityRow>`
            SELECT
                [agent_id], [name],
                [memories_created], [notes_created], [artifacts_created],
                [milestones_created], [tasks_created], [tags_created],
                [transitions_made], [memories_superseded],
                [last_action_at]
            FROM [dbo].[vw_Agent_Activity]
            WHERE [agent_id] = ${agentId}
        `.execute(this.ctx.kysely);

        return result.rows[0];

    }

    /**
     * Full audit trail for a Milestone — its own milestone-* transitions
     * plus every task-tracking transition for Tasks under it. Newest
     * first. Useful for the "what happened on this milestone" view.
     */
    async milestoneHistory(milestoneId: number) {

        return this.ctx.kysely
            .selectFrom('vw_StateTransition')
            .selectAll()
            .where('milestone_id', '=', milestoneId)
            .orderBy('occurred_at', 'desc')
            .orderBy('transition_id', 'desc')
            .execute();

    }

}
