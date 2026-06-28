-- Function: fn_IsRelevanceTransitionAllowed
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Returns true when (from_status, to_status) exists in RelevanceStatus_Allowed.
-- State machine gate for Memory, Note, Artifact, and Milestone relevance changes.

CREATE OR REPLACE FUNCTION "fn_IsRelevanceTransitionAllowed"(
    p_from_status VARCHAR(32),
    p_to_status   VARCHAR(32)
)
RETURNS BOOLEAN
LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM "RelevanceStatus_Allowed"
        WHERE from_status = p_from_status
          AND to_status   = p_to_status
    );
$$;
