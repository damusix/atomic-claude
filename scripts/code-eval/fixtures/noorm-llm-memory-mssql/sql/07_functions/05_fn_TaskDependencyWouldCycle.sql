-- =============================================================================
-- fn_TaskDependencyWouldCycle (milestone_id, task_no, dep_milestone_id, dep_task_no) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 if inserting the edge (origin -> dep) would close a cycle in the
-- Task_Dependency graph. Walks downstream from the dep node, following the
-- existing dep edges, and tests whether the origin node is reachable.
--
-- Reasoning:
--   The new edge being inserted is (origin -> dep). The dep is the upstream
--   side. If origin is reachable from dep through existing dependency edges,
--   the new edge would close the loop.
--
-- NO SCHEMABINDING: recursive CTEs in scalar UDFs work, but we keep this
-- function unbound for simplicity (per playbook).
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_TaskDependencyWouldCycle](
    @milestone_id     INT,
    @task_no          INT,
    @dep_milestone_id INT,
    @dep_task_no      INT
)
RETURNS BIT
AS
BEGIN
    DECLARE @cycles BIT = 0;

    WITH walk AS (
        -- Anchor: the dep node's outbound dependencies.
        SELECT [dep_milestone_id] AS m, [dep_task_no] AS t
        FROM [dbo].[Task_Dependency]
        WHERE [milestone_id] = @dep_milestone_id
          AND [task_no]      = @dep_task_no

        UNION ALL

        -- Recurse: follow each visited node's outbound deps.
        SELECT td.[dep_milestone_id], td.[dep_task_no]
        FROM [dbo].[Task_Dependency] td
        INNER JOIN walk w
                ON td.[milestone_id] = w.m
               AND td.[task_no]      = w.t
    )
    SELECT @cycles = CASE
        WHEN EXISTS (
            SELECT 1
            FROM walk
            WHERE m = @milestone_id
              AND t = @task_no
        ) THEN 1
        ELSE 0
    END
    OPTION (MAXRECURSION 1000);

    RETURN @cycles;
END;
