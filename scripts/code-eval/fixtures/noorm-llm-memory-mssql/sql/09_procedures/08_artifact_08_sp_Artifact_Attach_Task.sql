CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Attach_Task]
    @artifact_id  INT,
    @milestone_id INT,
    @task_no      INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Idempotent: skip if already attached
    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[Task_Artifact]
        WHERE [milestone_id] = @milestone_id
          AND [task_no]      = @task_no
          AND [artifact_id]  = @artifact_id
    )
    BEGIN
        INSERT INTO [dbo].[Task_Artifact] ([milestone_id], [task_no], [artifact_id])
            VALUES (@milestone_id, @task_no, @artifact_id);
    END
END
