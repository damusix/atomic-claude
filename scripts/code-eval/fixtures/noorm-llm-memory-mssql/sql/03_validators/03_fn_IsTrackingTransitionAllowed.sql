-- =============================================================================
-- fn_IsTrackingTransitionAllowed (from_status, to_status) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 when (from_status, to_status) is whitelisted in
-- TrackingStatus_Allowed. State-machine gate for Milestone and Task
-- tracking changes.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_IsTrackingTransitionAllowed](
    @from_status VARCHAR(32),
    @to_status   VARCHAR(32)
)
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    RETURN CASE
        WHEN EXISTS (
            SELECT 1
            FROM [dbo].[TrackingStatus_Allowed]
            WHERE [from_status] = @from_status
              AND [to_status]   = @to_status
        ) THEN 1
        ELSE 0
    END;
END;
