-- View: vw_Active_Memory
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Memory rows with relevance_status = 'active', plus the computed confidence
-- score (mirrors vw_Memory). The most common Memory read path.

DROP VIEW IF EXISTS "vw_Active_Memory" CASCADE;

CREATE VIEW "vw_Active_Memory" AS
    SELECT
        m.memory_id,
        m.content,
        m.reason,
        m.domain,
        m.category,
        m.relevance_status,
        m.provenance_id,
        m.agent_id,
        m.was_inferred,
        m.was_observed,
        m.was_evidenced,
        m.was_user_provided,
        "fn_MemoryConfidence"(m.memory_id) AS confidence,
        m.last_accessed_at,
        m.access_count,
        m.created_at,
        m.updated_at
    FROM "Memory" m
    WHERE m.relevance_status = 'active';
