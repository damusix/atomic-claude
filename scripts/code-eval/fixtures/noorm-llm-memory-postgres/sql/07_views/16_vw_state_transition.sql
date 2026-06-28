-- View: vw_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Unified audit history. StateTransition is exclusive over its 5 subtype tables;
-- this view LEFT JOINs all five and COALESCEs entity columns to 0 so callers can
-- pivot on state_transition_type to read the relevant entity column.

DROP VIEW IF EXISTS "vw_StateTransition" CASCADE;

CREATE VIEW "vw_StateTransition" AS
    SELECT
        st.transition_id,
        st.state_transition_type,
        st.agent_id,
        st.from_status,
        st.to_status,
        st.reason,
        st.occurred_at,
        st.created_at,
        COALESCE(mst.milestone_id, tst.milestone_id, 0) AS milestone_id,
        COALESCE(tst.task_no, 0)                        AS task_no,
        COALESCE(memst.memory_id, 0)                    AS memory_id,
        COALESCE(nst.note_id, 0)                        AS note_id,
        COALESCE(ast.artifact_id, 0)                    AS artifact_id
    FROM "StateTransition" st
    LEFT JOIN "Milestone_StateTransition" mst   ON mst.transition_id   = st.transition_id
    LEFT JOIN "Task_StateTransition"      tst   ON tst.transition_id   = st.transition_id
    LEFT JOIN "Memory_StateTransition"    memst ON memst.transition_id = st.transition_id
    LEFT JOIN "Note_StateTransition"      nst   ON nst.transition_id   = st.transition_id
    LEFT JOIN "Artifact_StateTransition"  ast   ON ast.transition_id   = st.transition_id;
