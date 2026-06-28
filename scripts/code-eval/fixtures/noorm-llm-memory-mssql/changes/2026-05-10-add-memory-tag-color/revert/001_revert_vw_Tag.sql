-- Revert vw_Tag to the pre-color shape (no [color] column).

CREATE OR ALTER VIEW [dbo].[vw_Tag]
AS
SELECT
    t.[tag_id],
    t.[name],
    t.[description],
    t.[reason],
    t.[provenance_id],
    CAST(N'project' AS VARCHAR(32)) AS [relation_type],
    pt.[project_id]                 AS [project_id],
    CAST(0 AS INT)                  AS [memory_id],
    CAST(0 AS INT)                  AS [artifact_id],
    CAST(0 AS INT)                  AS [milestone_id],
    CAST(0 AS INT)                  AS [task_no],
    pt.[created_at]                 AS [created_at]
FROM [dbo].[Tag] t
INNER JOIN [dbo].[Project_Tag] pt ON pt.[tag_id] = t.[tag_id]
UNION ALL
SELECT
    t.[tag_id],
    t.[name],
    t.[description],
    t.[reason],
    t.[provenance_id],
    CAST(N'memory' AS VARCHAR(32)) AS [relation_type],
    CAST(0 AS INT)                 AS [project_id],
    mt.[memory_id]                 AS [memory_id],
    CAST(0 AS INT)                 AS [artifact_id],
    CAST(0 AS INT)                 AS [milestone_id],
    CAST(0 AS INT)                 AS [task_no],
    mt.[created_at]                AS [created_at]
FROM [dbo].[Tag] t
INNER JOIN [dbo].[Memory_Tag] mt ON mt.[tag_id] = t.[tag_id]
UNION ALL
SELECT
    t.[tag_id],
    t.[name],
    t.[description],
    t.[reason],
    t.[provenance_id],
    CAST(N'artifact' AS VARCHAR(32)) AS [relation_type],
    CAST(0 AS INT)                   AS [project_id],
    CAST(0 AS INT)                   AS [memory_id],
    at.[artifact_id]                 AS [artifact_id],
    CAST(0 AS INT)                   AS [milestone_id],
    CAST(0 AS INT)                   AS [task_no],
    at.[created_at]                  AS [created_at]
FROM [dbo].[Tag] t
INNER JOIN [dbo].[Artifact_Tag] at ON at.[tag_id] = t.[tag_id]
UNION ALL
SELECT
    t.[tag_id],
    t.[name],
    t.[description],
    t.[reason],
    t.[provenance_id],
    CAST(N'milestone' AS VARCHAR(32)) AS [relation_type],
    CAST(0 AS INT)                    AS [project_id],
    CAST(0 AS INT)                    AS [memory_id],
    CAST(0 AS INT)                    AS [artifact_id],
    mst.[milestone_id]                AS [milestone_id],
    CAST(0 AS INT)                    AS [task_no],
    mst.[created_at]                  AS [created_at]
FROM [dbo].[Tag] t
INNER JOIN [dbo].[Milestone_Tag] mst ON mst.[tag_id] = t.[tag_id]
UNION ALL
SELECT
    t.[tag_id],
    t.[name],
    t.[description],
    t.[reason],
    t.[provenance_id],
    CAST(N'task' AS VARCHAR(32)) AS [relation_type],
    CAST(0 AS INT)               AS [project_id],
    CAST(0 AS INT)               AS [memory_id],
    CAST(0 AS INT)               AS [artifact_id],
    tt.[milestone_id]            AS [milestone_id],
    tt.[task_no]                 AS [task_no],
    tt.[created_at]              AS [created_at]
FROM [dbo].[Tag] t
INNER JOIN [dbo].[Task_Tag] tt ON tt.[tag_id] = t.[tag_id];
