-- =============================================================================
-- fn_MemoryRank (memory_id) -> FLOAT
-- -----------------------------------------------------------------------------
-- Composite retrieval score combining the signals already on Memory.
-- No embeddings -- pure scalar math:
--
--   confidence       = (was_inferred + was_observed + was_evidenced
--                       + was_user_provided) / 4.0          -- 0.0 to 1.0
--   recency_decay    = 1 / (1 + days_since_last_accessed / 30)
--   relevance_weight = 1.0 if 'active'
--                      0.5 if 'needs-review'
--                      0.1 if 'superseded'
--                      0.0 otherwise
--
--   rank = confidence * recency_decay * relevance_weight
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_MemoryRank](@memory_id INT)
RETURNS FLOAT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @inferred      BIT;
    DECLARE @observed      BIT;
    DECLARE @evidenced     BIT;
    DECLARE @user_provided BIT;
    DECLARE @last          DATETIME2(7);
    DECLARE @rs            VARCHAR(32);

    SELECT @inferred      = [was_inferred],
           @observed      = [was_observed],
           @evidenced     = [was_evidenced],
           @user_provided = [was_user_provided],
           @last          = [last_accessed_at],
           @rs            = [relevance_status]
    FROM [dbo].[Memory]
    WHERE [memory_id] = @memory_id;

    -- Memory not found -> rank 0.
    IF @last IS NULL
        RETURN 0.0;

    DECLARE @confidence FLOAT = (
        CAST(@inferred      AS FLOAT)
      + CAST(@observed      AS FLOAT)
      + CAST(@evidenced     AS FLOAT)
      + CAST(@user_provided AS FLOAT)
    ) / 4.0;

    DECLARE @days FLOAT = DATEDIFF(SECOND, @last, SYSUTCDATETIME()) / 86400.0;
    IF @days < 0.0 SET @days = 0.0;
    DECLARE @recency FLOAT = 1.0 / (1.0 + @days / 30.0);

    DECLARE @relevance FLOAT = CASE @rs
        WHEN 'active'       THEN 1.0
        WHEN 'needs-review' THEN 0.5
        WHEN 'superseded'   THEN 0.1
        ELSE 0.0
    END;

    RETURN @confidence * @recency * @relevance;
END;
