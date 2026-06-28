-- =============================================================================
-- vw_Recent_Activity
-- -----------------------------------------------------------------------------
-- Cross-entity activity stream. UNION ALL across all entity tables (one row
-- per created_at, plus one per updated_at when updated_at <> created_at) and
-- StateTransition (one row per occurred_at).
--
-- Columns (identical across every UNION branch):
--   entity_type      VARCHAR(32)  -- 'memory'|'note'|'milestone'|'task'|'artifact'|'tag'|'project'
--   entity_id        INT          -- elevated entity PK; 0 for tasks (use milestone_id+task_no)
--   milestone_id     INT          -- task identity component; 0 for non-task rows
--   task_no          INT          -- task identity component; 0 for non-task rows
--   title_or_excerpt NVARCHAR(255)
--   agent_id         INT
--   action_type      VARCHAR(32)  -- 'created' | 'updated' | 'transitioned'
--   occurred_at      DATETIME2
--
-- Tag has no agent_id column on the table -- it inherits from its provenance
-- pattern; we expose Tag.agent_id directly since it exists on the table.
-- StateTransition has no updated_at (immutable) -- only one row per transition.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Recent_Activity]
AS
-- Memory: created
SELECT
    CAST(N'memory' AS VARCHAR(32))                      AS [entity_type],
    m.[memory_id]                                       AS [entity_id],
    CAST(0 AS INT)                                      AS [milestone_id],
    CAST(0 AS INT)                                      AS [task_no],
    CAST(LEFT(m.[content], 80) AS NVARCHAR(255))        AS [title_or_excerpt],
    m.[agent_id]                                        AS [agent_id],
    CAST(N'created' AS VARCHAR(32))                     AS [action_type],
    m.[created_at]                                      AS [occurred_at]
FROM [dbo].[Memory] m
UNION ALL
-- Memory: updated
SELECT
    CAST(N'memory' AS VARCHAR(32)),
    m.[memory_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(LEFT(m.[content], 80) AS NVARCHAR(255)),
    m.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    m.[updated_at]
FROM [dbo].[Memory] m
WHERE m.[updated_at] <> m.[created_at]
UNION ALL
-- Note: created
SELECT
    CAST(N'note' AS VARCHAR(32)),
    n.[note_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(LEFT(n.[content], 80) AS NVARCHAR(255)),
    n.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    n.[created_at]
FROM [dbo].[Note] n
UNION ALL
-- Note: updated
SELECT
    CAST(N'note' AS VARCHAR(32)),
    n.[note_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(LEFT(n.[content], 80) AS NVARCHAR(255)),
    n.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    n.[updated_at]
FROM [dbo].[Note] n
WHERE n.[updated_at] <> n.[created_at]
UNION ALL
-- Milestone: created
SELECT
    CAST(N'milestone' AS VARCHAR(32)),
    ms.[milestone_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(ms.[title] AS NVARCHAR(255)),
    ms.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    ms.[created_at]
FROM [dbo].[Milestone] ms
UNION ALL
-- Milestone: updated
SELECT
    CAST(N'milestone' AS VARCHAR(32)),
    ms.[milestone_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(ms.[title] AS NVARCHAR(255)),
    ms.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    ms.[updated_at]
FROM [dbo].[Milestone] ms
WHERE ms.[updated_at] <> ms.[created_at]
UNION ALL
-- Task: created  (composite PK -> entity_id = 0; consumers use milestone_id+task_no)
SELECT
    CAST(N'task' AS VARCHAR(32)),
    CAST(0 AS INT)                AS [entity_id],
    t.[milestone_id]              AS [milestone_id],
    t.[task_no]                   AS [task_no],
    CAST(t.[title] AS NVARCHAR(255)),
    t.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    t.[created_at]
FROM [dbo].[Task] t
UNION ALL
-- Task: updated
SELECT
    CAST(N'task' AS VARCHAR(32)),
    CAST(0 AS INT),
    t.[milestone_id],
    t.[task_no],
    CAST(t.[title] AS NVARCHAR(255)),
    t.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    t.[updated_at]
FROM [dbo].[Task] t
WHERE t.[updated_at] <> t.[created_at]
UNION ALL
-- Artifact: created
SELECT
    CAST(N'artifact' AS VARCHAR(32)),
    a.[artifact_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(a.[title] AS NVARCHAR(255)),
    a.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    a.[created_at]
FROM [dbo].[Artifact] a
UNION ALL
-- Artifact: updated
SELECT
    CAST(N'artifact' AS VARCHAR(32)),
    a.[artifact_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(a.[title] AS NVARCHAR(255)),
    a.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    a.[updated_at]
FROM [dbo].[Artifact] a
WHERE a.[updated_at] <> a.[created_at]
UNION ALL
-- Tag: created
SELECT
    CAST(N'tag' AS VARCHAR(32)),
    tg.[tag_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(tg.[name] AS NVARCHAR(255)),
    tg.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    tg.[created_at]
FROM [dbo].[Tag] tg
UNION ALL
-- Tag: updated
SELECT
    CAST(N'tag' AS VARCHAR(32)),
    tg.[tag_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(tg.[name] AS NVARCHAR(255)),
    tg.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    tg.[updated_at]
FROM [dbo].[Tag] tg
WHERE tg.[updated_at] <> tg.[created_at]
UNION ALL
-- Project: created
SELECT
    CAST(N'project' AS VARCHAR(32)),
    p.[project_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(p.[name] AS NVARCHAR(255)),
    p.[agent_id],
    CAST(N'created' AS VARCHAR(32)),
    p.[created_at]
FROM [dbo].[Project] p
UNION ALL
-- Project: updated
SELECT
    CAST(N'project' AS VARCHAR(32)),
    p.[project_id],
    CAST(0 AS INT),
    CAST(0 AS INT),
    CAST(p.[name] AS NVARCHAR(255)),
    p.[agent_id],
    CAST(N'updated' AS VARCHAR(32)),
    p.[updated_at]
FROM [dbo].[Project] p
WHERE p.[updated_at] <> p.[created_at]
UNION ALL
-- StateTransition: transitioned (one row per transition; immutable, no updated_at)
SELECT
    CAST(N'transition' AS VARCHAR(32))                     AS [entity_type],
    st.[transition_id]                                     AS [entity_id],
    CAST(0 AS INT)                                         AS [milestone_id],
    CAST(0 AS INT)                                         AS [task_no],
    CAST(LEFT(st.[reason], 80) AS NVARCHAR(255))           AS [title_or_excerpt],
    st.[agent_id]                                          AS [agent_id],
    CAST(N'transitioned' AS VARCHAR(32))                   AS [action_type],
    st.[occurred_at]                                       AS [occurred_at]
FROM [dbo].[StateTransition] st;
