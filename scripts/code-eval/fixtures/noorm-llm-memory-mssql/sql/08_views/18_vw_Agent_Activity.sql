-- =============================================================================
-- vw_Agent_Activity
-- -----------------------------------------------------------------------------
-- Per-agent rollup. One row per Agent (including the sentinel Agent(0)).
-- Counts come from each tracked entity table; last_action_at takes the MAX
-- across StateTransition.occurred_at and every entity's updated_at.
--
-- Columns:
--   agent_id, name,
--   memories_created, notes_created, artifacts_created,
--   milestones_created, tasks_created, tags_created,
--   transitions_made, memories_superseded,
--   last_action_at.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Agent_Activity]
AS
SELECT
    a.[agent_id],
    a.[name],
    (SELECT COUNT(*) FROM [dbo].[Memory]    WHERE [agent_id] = a.[agent_id]) AS [memories_created],
    (SELECT COUNT(*) FROM [dbo].[Note]      WHERE [agent_id] = a.[agent_id]) AS [notes_created],
    (SELECT COUNT(*) FROM [dbo].[Artifact]  WHERE [agent_id] = a.[agent_id]) AS [artifacts_created],
    (SELECT COUNT(*) FROM [dbo].[Milestone] WHERE [agent_id] = a.[agent_id]) AS [milestones_created],
    (SELECT COUNT(*) FROM [dbo].[Task]      WHERE [agent_id] = a.[agent_id]) AS [tasks_created],
    (SELECT COUNT(*) FROM [dbo].[Tag]       WHERE [agent_id] = a.[agent_id]) AS [tags_created],
    (SELECT COUNT(*) FROM [dbo].[StateTransition] WHERE [agent_id] = a.[agent_id]) AS [transitions_made],
    (SELECT COUNT(*) FROM [dbo].[StateTransition]
        WHERE [agent_id] = a.[agent_id]
          AND [state_transition_type] = N'memory-relevance'
          AND [to_status]             = N'superseded') AS [memories_superseded],
    (SELECT MAX(d) FROM (VALUES
        ((SELECT MAX([occurred_at]) FROM [dbo].[StateTransition] WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Memory]          WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Note]            WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Milestone]       WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Task]            WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Tag]             WHERE [agent_id] = a.[agent_id])),
        ((SELECT MAX([updated_at])  FROM [dbo].[Artifact]        WHERE [agent_id] = a.[agent_id]))
    ) AS m(d)) AS [last_action_at]
FROM [dbo].[Agent] a;
