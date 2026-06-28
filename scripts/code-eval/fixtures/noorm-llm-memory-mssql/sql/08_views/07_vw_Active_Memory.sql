-- =============================================================================
-- vw_Active_Memory
-- -----------------------------------------------------------------------------
-- vw_Memory filtered to relevance_status = 'active'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Active_Memory]
AS
SELECT *
FROM [dbo].[vw_Memory]
WHERE [relevance_status] = N'active';
