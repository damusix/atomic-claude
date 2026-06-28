-- =============================================================================
-- vw_Milestone_Stats
-- -----------------------------------------------------------------------------
-- One row per milestone with rollup counts. Uses correlated subqueries to
-- avoid cartesian explosion across multiple LEFT JOIN aggregations.
--
-- Columns:
--   milestone_id, title, content, reason,
--   tracking_status, relevance_status, provenance_id,
--   total_tasks, open_tasks, done_tasks, abandoned_tasks, blocked_tasks,
--   total_artifacts (Milestone_Artifact + Task_Artifact),
--   total_notes (Milestone_Note + Task_Note),
--   total_tags  (Milestone_Tag + Task_Tag),
--   total_dependencies (Task_Dependency rows where this milestone is on
--     either side -- the dependent or the depended-upon),
--   project_count (Project_Milestone),
--   created_at, updated_at.
--
-- blocked_tasks counts DISTINCT tasks under this milestone that have at least
-- one Task_Dependency of verb 'blocks' whose dependency target is not 'done'.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Milestone_Stats]
AS
SELECT
    m.[milestone_id],
    m.[title],
    m.[content],
    m.[reason],
    m.[tracking_status],
    m.[relevance_status],
    m.[provenance_id],
    (SELECT COUNT(*)
        FROM [dbo].[Task] t
        WHERE t.[milestone_id] = m.[milestone_id]) AS [total_tasks],
    (SELECT COUNT(*)
        FROM [dbo].[Task] t
        WHERE t.[milestone_id] = m.[milestone_id]
          AND [dbo].[fn_IsOpen](t.[tracking_status]) = 1) AS [open_tasks],
    (SELECT COUNT(*)
        FROM [dbo].[Task] t
        WHERE t.[milestone_id] = m.[milestone_id]
          AND t.[tracking_status] = N'done') AS [done_tasks],
    (SELECT COUNT(*)
        FROM [dbo].[Task] t
        WHERE t.[milestone_id] = m.[milestone_id]
          AND t.[tracking_status] = N'abandoned') AS [abandoned_tasks],
    (SELECT COUNT(DISTINCT t.[task_no])
        FROM [dbo].[Task] t
        INNER JOIN [dbo].[Task_Dependency] td
            ON td.[milestone_id] = t.[milestone_id]
           AND td.[task_no]      = t.[task_no]
        INNER JOIN [dbo].[Task] dt
            ON dt.[milestone_id] = td.[dep_milestone_id]
           AND dt.[task_no]      = td.[dep_task_no]
        WHERE t.[milestone_id]   = m.[milestone_id]
          AND td.[dependency_verb] = N'blocks'
          AND dt.[tracking_status] <> N'done') AS [blocked_tasks],
    ((SELECT COUNT(*)
        FROM [dbo].[Milestone_Artifact] ma
        WHERE ma.[milestone_id] = m.[milestone_id])
     + (SELECT COUNT(*)
        FROM [dbo].[Task_Artifact] ta
        WHERE ta.[milestone_id] = m.[milestone_id])) AS [total_artifacts],
    ((SELECT COUNT(*)
        FROM [dbo].[Milestone_Note] mn
        WHERE mn.[milestone_id] = m.[milestone_id])
     + (SELECT COUNT(*)
        FROM [dbo].[Task_Note] tn
        WHERE tn.[milestone_id] = m.[milestone_id])) AS [total_notes],
    ((SELECT COUNT(*)
        FROM [dbo].[Milestone_Tag] mtg
        WHERE mtg.[milestone_id] = m.[milestone_id])
     + (SELECT COUNT(*)
        FROM [dbo].[Task_Tag] ttg
        WHERE ttg.[milestone_id] = m.[milestone_id])) AS [total_tags],
    (SELECT COUNT(*)
        FROM [dbo].[Task_Dependency] td
        WHERE td.[milestone_id] = m.[milestone_id]
           OR td.[dep_milestone_id] = m.[milestone_id]) AS [total_dependencies],
    (SELECT COUNT(*)
        FROM [dbo].[Project_Milestone] pm
        WHERE pm.[milestone_id] = m.[milestone_id]) AS [project_count],
    m.[created_at],
    m.[updated_at]
FROM [dbo].[Milestone] m;
