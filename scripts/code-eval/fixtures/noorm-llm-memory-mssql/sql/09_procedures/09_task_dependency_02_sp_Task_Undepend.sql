CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Undepend]
    @milestone_id     INT,
    @task_no          INT,
    @dep_milestone_id INT,
    @dep_task_no      INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Silent on missing row: undepend is idempotent.
    DELETE FROM [dbo].[Task_Dependency]
        WHERE [milestone_id]     = @milestone_id
          AND [task_no]          = @task_no
          AND [dep_milestone_id] = @dep_milestone_id
          AND [dep_task_no]      = @dep_task_no;
END
