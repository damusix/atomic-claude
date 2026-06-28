-- Function: fn_IsActive
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Returns true when relevance_status = 'active'. Convenience filter.

CREATE OR REPLACE FUNCTION "fn_IsActive"(p_relevance_status VARCHAR(32))
RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE
AS $$
    SELECT p_relevance_status = 'active';
$$;
