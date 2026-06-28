-- Function: fn_IsTrackingTransitionAllowed
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Returns true when (from_status, to_status) exists in TrackingStatus_Allowed.
-- State machine gate for Milestone and Task tracking changes.

CREATE OR REPLACE FUNCTION "fn_IsTrackingTransitionAllowed"(
    p_from_status VARCHAR(32),
    p_to_status   VARCHAR(32)
)
RETURNS BOOLEAN
LANGUAGE sql STABLE
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM "TrackingStatus_Allowed"
        WHERE from_status = p_from_status
          AND to_status   = p_to_status
    );
$$;
