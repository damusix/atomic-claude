-- =============================================================================
-- fn_IsActive (relevance_status) -> BIT
-- -----------------------------------------------------------------------------
-- Pure scalar. Returns 1 when relevance_status = 'active'.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_IsActive](@relevance_status VARCHAR(32))
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    RETURN CASE WHEN @relevance_status = 'active' THEN 1 ELSE 0 END;
END;
