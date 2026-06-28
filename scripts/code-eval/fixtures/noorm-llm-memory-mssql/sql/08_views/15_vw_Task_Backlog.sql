-- =============================================================================
-- vw_Task_Backlog
-- -----------------------------------------------------------------------------
-- Tasks under active milestones whose tracking_status is open. Includes a
-- computed is_blocked flag: 1 when at least one outgoing 'blocks' dependency
-- targets a task whose tracking_status <> 'done'.
--
-- Columns:
--   milestone_id, task_no, title, content, tracking_status, agent_id,
--   is_blocked (BIT), created_at, updated_at.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Task_Backlog]
AS
SELECT
    t.[milestone_id],
    t.[task_no],
    t.[title],
    t.[content],
    t.[tracking_status],
    t.[agent_id],
    CAST(CASE WHEN EXISTS (
        SELECT 1
        FROM [dbo].[Task_Dependency] td
        INNER JOIN [dbo].[Task] dep
            ON dep.[milestone_id] = td.[dep_milestone_id]
           AND dep.[task_no]      = td.[dep_task_no]
        WHERE td.[milestone_id]   = t.[milestone_id]
          AND td.[task_no]        = t.[task_no]
          AND td.[dependency_verb] = N'blocks'
          AND dep.[tracking_status] <> N'done'
    ) THEN 1 ELSE 0 END AS BIT) AS [is_blocked],
    t.[created_at],
    t.[updated_at]
FROM [dbo].[Task] t
INNER JOIN [dbo].[Milestone] m ON m.[milestone_id] = t.[milestone_id]
WHERE m.[relevance_status] = N'active'
  AND [dbo].[fn_IsOpen](t.[tracking_status]) = 1;
