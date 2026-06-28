-- Function: fn_MemoryRank
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Composite retrieval score combining the signals already on Memory.
-- No embeddings, pure scalar math:
--
--   confidence       = fn_MemoryConfidence(memory_id) / 4.0      -- 0.0–1.0
--   recency_decay    = 1 / (1 + days_since_last_accessed / 30)   -- decays over weeks
--   relevance_weight = 1.0  if relevance_status = 'active'
--                      0.5  if 'needs-review'
--                      0.1  if 'superseded'
--                      0.0  otherwise
--
--   rank = confidence * recency_decay * relevance_weight
--
-- Use: ORDER BY fn_MemoryRank(memory_id) DESC for default retrieval ranking.

CREATE OR REPLACE FUNCTION "fn_MemoryRank"(p_memory_id INT)
RETURNS DOUBLE PRECISION
LANGUAGE plpgsql STABLE
AS $$
DECLARE
    v_confidence_raw     INT;
    v_last_accessed_at   TIMESTAMPTZ;
    v_relevance_status   VARCHAR(32);
    v_confidence         DOUBLE PRECISION;
    v_days_since         DOUBLE PRECISION;
    v_recency_decay      DOUBLE PRECISION;
    v_relevance_weight   DOUBLE PRECISION;
BEGIN
    SELECT
        last_accessed_at,
        relevance_status
    INTO
        v_last_accessed_at,
        v_relevance_status
    FROM "Memory"
    WHERE memory_id = p_memory_id;

    -- Missing row: rank is 0.
    IF NOT FOUND THEN
        RETURN 0.0;
    END IF;

    -- Confidence component: 0.0–1.0
    v_confidence_raw := "fn_MemoryConfidence"(p_memory_id);
    v_confidence     := v_confidence_raw::double precision / 4.0;

    -- Recency component: decays over ~weeks. NULL last_accessed_at -> treat as never accessed (max decay).
    IF v_last_accessed_at IS NULL THEN
        v_recency_decay := 0.0;
    ELSE
        v_days_since    := EXTRACT(EPOCH FROM (NOW() - v_last_accessed_at)) / 86400.0;
        v_recency_decay := 1.0 / (1.0 + v_days_since / 30.0);
    END IF;

    -- Relevance component
    v_relevance_weight := CASE v_relevance_status
        WHEN 'active'       THEN 1.0
        WHEN 'needs-review' THEN 0.5
        WHEN 'superseded'   THEN 0.1
        ELSE 0.0
    END;

    RETURN v_confidence * v_recency_decay * v_relevance_weight;
END;
$$;
