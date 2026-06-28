-- =============================================================================
-- fn_NextTaskNo (milestone_id) -> INT
-- -----------------------------------------------------------------------------
-- Returns the next task_no for a Task under the given milestone.
-- MAX(task_no) + 1, scoped to milestone_id. Returns 1 when no tasks exist.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_NextTaskNo](@milestone_id INT)
RETURNS INT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @next INT;

    SELECT @next = COALESCE(MAX([task_no]), 0) + 1
    FROM [dbo].[Task]
    WHERE [milestone_id] = @milestone_id;

    RETURN @next;
END;
