-- =============================================================================
-- vw_Note
-- -----------------------------------------------------------------------------
-- One row per note. Notes are exclusive subtypes -- exactly one *_Note table
-- matches per note_id (enforced by INSTEAD OF triggers). LEFT JOIN to all
-- three subtype tables and COALESCE missing entity columns to 0.
--
-- Columns:
--   note_id, note_type, content, reason,
--   relevance_status, provenance_id,
--   project_id   (0 unless note_type = 'project'),
--   milestone_id (0 unless note_type = 'milestone' or 'task'),
--   task_no      (0 unless note_type = 'task').
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Note]
AS
SELECT
    n.[note_id],
    n.[note_type],
    n.[content],
    n.[reason],
    n.[relevance_status],
    n.[provenance_id],
    COALESCE(pn.[project_id], 0)                    AS [project_id],
    COALESCE(mn.[milestone_id], tn.[milestone_id], 0) AS [milestone_id],
    COALESCE(tn.[task_no], 0)                       AS [task_no]
FROM [dbo].[Note] n
LEFT JOIN [dbo].[Project_Note]   pn ON pn.[note_id] = n.[note_id]
LEFT JOIN [dbo].[Milestone_Note] mn ON mn.[note_id] = n.[note_id]
LEFT JOIN [dbo].[Task_Note]      tn ON tn.[note_id] = n.[note_id];
