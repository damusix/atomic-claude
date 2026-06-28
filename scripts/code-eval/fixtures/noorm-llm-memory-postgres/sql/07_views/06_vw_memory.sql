-- View: vw_Memory
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Memory plus computed confidence (0-4 = count of true grounding flags).
-- Confidence is computed by fn_MemoryConfidence so callers stay decoupled
-- from the underlying boolean columns.

DROP VIEW IF EXISTS "vw_Memory" CASCADE;

CREATE VIEW "vw_Memory" AS
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
    FROM "Memory" m;
