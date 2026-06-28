-- =============================================================================
-- Task procedures (hierarchic child of Milestone; PK = milestone_id, task_no)
-- -----------------------------------------------------------------------------
-- sp_Task_Create       — insert with MAX+1 task_no scoped to milestone
-- sp_Task_Update       — modify metadata (tracking changes use SetTracking)
-- sp_Task_SetTracking  — gated tracking_status change + StateTransition log
-- sp_Task_Delete       — soft-delete attached Notes + transition to abandoned
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Create]
    @milestone_id INT,
    @title        NVARCHAR(255),
    @content      NVARCHAR(MAX) = N'',
    @reason       NVARCHAR(255) = N'',
    @agent_id     INT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @task_no INT = [dbo].[fn_NextTaskNo](@milestone_id);

    INSERT INTO [dbo].[Task]
            ([milestone_id], [task_no], [tracking_status], [agent_id],
             [title], [content], [reason])
        VALUES
            (@milestone_id, @task_no, 'not-started', @agent_id,
             @title, @content, @reason);

    SELECT @milestone_id AS [milestone_id], @task_no AS [task_no];
END
