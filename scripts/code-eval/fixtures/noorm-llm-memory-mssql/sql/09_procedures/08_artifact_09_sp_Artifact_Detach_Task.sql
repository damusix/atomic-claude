CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Detach_Task]
    @artifact_id  INT,
    @milestone_id INT,
    @task_no      INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Silent on missing row: detach is idempotent.
    DELETE FROM [dbo].[Task_Artifact]
        WHERE [milestone_id] = @milestone_id
          AND [task_no]      = @task_no
          AND [artifact_id]  = @artifact_id;
END
