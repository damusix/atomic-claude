-- View: vw_Active_Note
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Note rows with relevance_status = 'active'. Selects the base Note table
-- (not vw_Note) — callers who need ownership columns can join vw_Note instead.

DROP VIEW IF EXISTS "vw_Active_Note" CASCADE;

CREATE VIEW "vw_Active_Note" AS
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
    WHERE n.relevance_status = 'active';
