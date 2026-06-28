-- View: vw_Deleted_Milestone
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Milestone rows with relevance_status = 'deleted'.

DROP VIEW IF EXISTS "vw_Deleted_Milestone" CASCADE;

CREATE VIEW "vw_Deleted_Milestone" AS
    SELECT
        m.milestone_id,
        m.tracking_status,
        m.relevance_status,
        m.provenance_id,
        m.agent_id,
        m.title,
        m.content,
        m.reason,
        m.created_at,
        m.updated_at
    FROM "Milestone" m
    WHERE m.relevance_status = 'deleted';
