-- =============================================================================
-- fn_IsOpen (tracking_status) -> BIT
-- -----------------------------------------------------------------------------
-- Pure scalar. Returns 1 when work is still actionable -- i.e., the
-- tracking_status is NOT 'done' and NOT 'abandoned'.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_IsOpen](@tracking_status VARCHAR(32))
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    RETURN CASE
        WHEN @tracking_status NOT IN ('done', 'abandoned') THEN 1
        ELSE 0
    END;
END;
