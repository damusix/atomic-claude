-- =============================================================================
-- fn_MemoryConfidence (memory_id) -> INT
-- -----------------------------------------------------------------------------
-- Returns the count of true confidence booleans on the Memory row.
-- Range: 0-4. Each flag is a verifiable knowledge-source claim:
--   was_inferred + was_observed + was_evidenced + was_user_provided
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_MemoryConfidence](@memory_id INT)
RETURNS INT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @count INT;

    SELECT @count = CAST([was_inferred]      AS INT)
                  + CAST([was_observed]      AS INT)
                  + CAST([was_evidenced]     AS INT)
                  + CAST([was_user_provided] AS INT)
    FROM [dbo].[Memory]
    WHERE [memory_id] = @memory_id;

    RETURN COALESCE(@count, 0);
END;
