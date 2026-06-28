-- =============================================================================
-- vw_Memory
-- -----------------------------------------------------------------------------
-- Memory plus the computed confidence score (count of true confidence
-- booleans, range 0-4). The most common Memory read.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Memory]
AS
SELECT
    m.[memory_id],
    m.[content],
    m.[reason],
    m.[domain],
    m.[category],
    m.[relevance_status],
    m.[provenance_id],
    m.[agent_id],
    m.[was_inferred],
    m.[was_observed],
    m.[was_evidenced],
    m.[was_user_provided],
    [dbo].[fn_MemoryConfidence](m.[memory_id]) AS [confidence],
    m.[last_accessed_at],
    m.[access_count],
    m.[created_at],
    m.[updated_at]
FROM [dbo].[Memory] m;
