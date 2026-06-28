-- View: vw_Deleted_Note
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Note rows with relevance_status = 'deleted'.

DROP VIEW IF EXISTS "vw_Deleted_Note" CASCADE;

CREATE VIEW "vw_Deleted_Note" AS
    SELECT
        n.note_id,
        n.note_type,
        n.relevance_status,
        n.provenance_id,
        n.agent_id,
        n.content,
        n.reason,
        n.created_at,
        n.updated_at
    FROM "Note" n
    WHERE n.relevance_status = 'deleted';
