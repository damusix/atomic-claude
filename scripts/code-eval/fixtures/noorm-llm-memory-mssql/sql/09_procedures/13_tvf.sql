-- =============================================================================
-- tvf_FilterMemoriesByTags
-- -----------------------------------------------------------------------------
-- Inline TVF returning (memory_id, content, rank) for memories that have ALL
-- of the requested tag_ids attached. Implemented with the relational division
-- pattern (double NOT EXISTS): "no requested tag is missing from this memory's
-- attachments".
--
-- The trailing EXISTS guard ensures an empty TVP returns zero rows rather
-- than every Memory in the database (the inner double-NOT-EXISTS is vacuously
-- true when @TagIds is empty).
--
-- rank is computed via fn_MemoryRank — recency-decayed confidence, scaled by
-- the relevance weight. Caller can ORDER BY [rank] DESC for ranked retrieval.
-- =============================================================================


CREATE OR ALTER FUNCTION [dbo].[tvf_FilterMemoriesByTags]
(
    @TagIds [dbo].[TagIdSet] READONLY
)
RETURNS TABLE
AS RETURN
(
    SELECT
        m.[memory_id],
        m.[content],
        [dbo].[fn_MemoryRank](m.[memory_id]) AS [rank]
    FROM [dbo].[Memory] m
    WHERE EXISTS (SELECT 1 FROM @TagIds)
      AND NOT EXISTS (
        SELECT 1
        FROM @TagIds t
        WHERE NOT EXISTS (
            SELECT 1
            FROM [dbo].[Memory_Tag] mt
            WHERE mt.[tag_id]    = t.[tag_id]
              AND mt.[memory_id] = m.[memory_id]
        )
      )
);
