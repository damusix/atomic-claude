-- Function: fn_IsOpen
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Returns true when tracking_status is NOT 'done' and NOT 'abandoned'.
-- In other words, the work is still actionable.

CREATE OR REPLACE FUNCTION "fn_IsOpen"(p_tracking_status VARCHAR(32))
RETURNS BOOLEAN
LANGUAGE sql IMMUTABLE
AS $$
    SELECT p_tracking_status NOT IN ('done', 'abandoned');
$$;
