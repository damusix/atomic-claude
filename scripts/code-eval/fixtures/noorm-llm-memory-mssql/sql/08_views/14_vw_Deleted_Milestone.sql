-- =============================================================================
-- vw_Deleted_Milestone
-- -----------------------------------------------------------------------------
-- Milestone rows where relevance_status = 'deleted'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Deleted_Milestone]
AS
SELECT
    m.[milestone_id],
    m.[tracking_status],
    m.[relevance_status],
    m.[provenance_id],
    m.[agent_id],
    m.[title],
    m.[content],
    m.[reason],
    m.[created_at],
    m.[updated_at]
FROM [dbo].[Milestone] m
WHERE m.[relevance_status] = N'deleted';
