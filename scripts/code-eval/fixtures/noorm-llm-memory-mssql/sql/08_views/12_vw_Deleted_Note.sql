-- =============================================================================
-- vw_Deleted_Note
-- -----------------------------------------------------------------------------
-- Note rows where relevance_status = 'deleted'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Deleted_Note]
AS
SELECT
    n.[note_id],
    n.[note_type],
    n.[relevance_status],
    n.[provenance_id],
    n.[agent_id],
    n.[content],
    n.[reason],
    n.[created_at],
    n.[updated_at]
FROM [dbo].[Note] n
WHERE n.[relevance_status] = N'deleted';
