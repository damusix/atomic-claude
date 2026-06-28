-- =============================================================================
-- vw_Artifact
-- -----------------------------------------------------------------------------
-- One row per artifact-entity attachment. Artifacts with zero attachments do
-- not appear. UNION ALL across Milestone_Artifact + Task_Artifact.
--
-- Columns:
--   artifact_id, title, description, filepath, reason,
--   relevance_status, provenance_id,
--   relation_type ('milestone'|'task'),
--   milestone_id (always populated -- tasks live under milestones),
--   task_no (populated when relation_type = 'task'),
--   created_at (from the join row).
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Artifact]
AS
SELECT
    a.[artifact_id],
    a.[title],
    a.[description],
    a.[filepath],
    a.[reason],
    a.[relevance_status],
    a.[provenance_id],
    CAST(N'milestone' AS VARCHAR(32)) AS [relation_type],
    ma.[milestone_id]                 AS [milestone_id],
    CAST(0 AS INT)                    AS [task_no],
    ma.[created_at]                   AS [created_at]
FROM [dbo].[Artifact] a
INNER JOIN [dbo].[Milestone_Artifact] ma ON ma.[artifact_id] = a.[artifact_id]
UNION ALL
SELECT
    a.[artifact_id],
    a.[title],
    a.[description],
    a.[filepath],
    a.[reason],
    a.[relevance_status],
    a.[provenance_id],
    CAST(N'task' AS VARCHAR(32)) AS [relation_type],
    ta.[milestone_id]            AS [milestone_id],
    ta.[task_no]                 AS [task_no],
    ta.[created_at]              AS [created_at]
FROM [dbo].[Artifact] a
INNER JOIN [dbo].[Task_Artifact] ta ON ta.[artifact_id] = a.[artifact_id];
