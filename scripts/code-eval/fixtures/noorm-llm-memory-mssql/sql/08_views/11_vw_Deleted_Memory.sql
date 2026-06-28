-- =============================================================================
-- vw_Deleted_Memory
-- -----------------------------------------------------------------------------
-- vw_Memory filtered to relevance_status = 'deleted'. The recovery surface --
-- soft-deleted memories that may be restored before sp_Cleanup hard-deletes
-- them past the TTL.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Deleted_Memory]
AS
SELECT *
FROM [dbo].[vw_Memory]
WHERE [relevance_status] = N'deleted';
