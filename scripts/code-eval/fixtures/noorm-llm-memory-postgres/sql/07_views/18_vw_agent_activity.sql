-- View: vw_Agent_Activity
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Per-agent rollup. One row per agent, even agents with no activity yet —
-- LEFT JOIN to grouped subqueries with COALESCE-to-0 keeps the no-NULL
-- invariant. last_action_at falls back to the epoch when the agent has never
-- transitioned anything.

DROP VIEW IF EXISTS "vw_Agent_Activity" CASCADE;

CREATE VIEW "vw_Agent_Activity" AS
    SELECT
        a.agent_id,
        a.name,
        COALESCE(mc.cnt,  0) AS memories_created,
        COALESCE(nc.cnt,  0) AS notes_created,
        COALESCE(ac.cnt,  0) AS artifacts_created,
        COALESCE(mic.cnt, 0) AS milestones_created,
        COALESCE(tc.cnt,  0) AS tasks_created,
        COALESCE(tgc.cnt, 0) AS tags_created,
        COALESCE(stc.cnt, 0) AS transitions_made,
        COALESCE(msc.cnt, 0) AS memories_superseded,
        COALESCE(la.last_action_at, '1970-01-01 00:00:00+00'::TIMESTAMPTZ) AS last_action_at
    FROM "Agent" a
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Memory"    GROUP BY agent_id
    ) mc  ON mc.agent_id  = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Note"      GROUP BY agent_id
    ) nc  ON nc.agent_id  = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Artifact"  GROUP BY agent_id
    ) ac  ON ac.agent_id  = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Milestone" GROUP BY agent_id
    ) mic ON mic.agent_id = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Task"      GROUP BY agent_id
    ) tc  ON tc.agent_id  = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "Tag"       GROUP BY agent_id
    ) tgc ON tgc.agent_id = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt FROM "StateTransition" GROUP BY agent_id
    ) stc ON stc.agent_id = a.agent_id
    LEFT JOIN (
        SELECT agent_id, COUNT(*)::INT AS cnt
        FROM "StateTransition"
        WHERE state_transition_type = 'memory-relevance'
          AND to_status = 'superseded'
        GROUP BY agent_id
    ) msc ON msc.agent_id = a.agent_id
    LEFT JOIN (
        SELECT agent_id, MAX(occurred_at) AS last_action_at
        FROM "StateTransition"
        GROUP BY agent_id
    ) la  ON la.agent_id  = a.agent_id;
