-- Function: fn_MemoryConfidence
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Returns the count of true confidence booleans on the Memory row.
-- Range: 0–4. Each boolean is a verifiable claim:
--   was_inferred + was_observed + was_evidenced + was_user_provided
-- Higher = more grounded in evidence.
--
-- COALESCE to 0 so the function never errors on a missing memory_id.

CREATE OR REPLACE FUNCTION "fn_MemoryConfidence"(p_memory_id INT)
RETURNS INT
LANGUAGE sql STABLE
AS $$
    SELECT COALESCE(
        (
            SELECT
                  was_inferred::int
                + was_observed::int
                + was_evidenced::int
                + was_user_provided::int
            FROM "Memory"
            WHERE memory_id = p_memory_id
        ),
        0
    );
$$;
