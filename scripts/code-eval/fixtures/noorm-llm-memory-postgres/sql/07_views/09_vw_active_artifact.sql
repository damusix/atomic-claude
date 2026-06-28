-- View: vw_Active_Artifact
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Artifact rows with relevance_status = 'active'. Selects the base Artifact table.

DROP VIEW IF EXISTS "vw_Active_Artifact" CASCADE;

CREATE VIEW "vw_Active_Artifact" AS
    SELECT
        a.artifact_id,
        a.relevance_status,
        a.provenance_id,
        a.agent_id,
        a.title,
        a.description,
        a.filepath,
        a.reason,
        a.created_at,
        a.updated_at
    FROM "Artifact" a
    WHERE a.relevance_status = 'active';
