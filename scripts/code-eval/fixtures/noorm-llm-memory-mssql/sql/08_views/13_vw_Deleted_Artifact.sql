-- =============================================================================
-- vw_Deleted_Artifact
-- -----------------------------------------------------------------------------
-- Artifact rows where relevance_status = 'deleted'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Deleted_Artifact]
AS
SELECT
    a.[artifact_id],
    a.[relevance_status],
    a.[provenance_id],
    a.[agent_id],
    a.[title],
    a.[description],
    a.[filepath],
    a.[reason],
    a.[created_at],
    a.[updated_at]
FROM [dbo].[Artifact] a
WHERE a.[relevance_status] = N'deleted';
