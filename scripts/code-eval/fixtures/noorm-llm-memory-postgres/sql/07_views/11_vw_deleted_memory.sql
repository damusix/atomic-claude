-- View: vw_Deleted_Memory
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Memory rows with relevance_status = 'deleted'. The recovery surface — the
-- LLM browses this to find soft-deleted memories before sp_Cleanup hard-deletes
-- them past the TTL. Includes the computed confidence score (mirrors vw_Memory).

DROP VIEW IF EXISTS "vw_Deleted_Memory" CASCADE;

CREATE VIEW "vw_Deleted_Memory" AS
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
    WHERE m.relevance_status = 'deleted';
