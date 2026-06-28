-- =============================================================================
-- vw_Active_Note
-- -----------------------------------------------------------------------------
-- Note rows where relevance_status = 'active'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Active_Note]
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
WHERE n.[relevance_status] = N'active';
