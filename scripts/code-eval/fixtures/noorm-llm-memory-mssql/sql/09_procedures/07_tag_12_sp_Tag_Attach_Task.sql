-- ---------------------------------------------------------------------------
-- Attach / Detach — Task (composite PK: tag_id + milestone_id + task_no)
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Attach_Task]
    @tag_id       INT,
    @milestone_id INT,
    @task_no      INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Task_Tag]
        WHERE [tag_id]       = @tag_id
          AND [milestone_id] = @milestone_id
          AND [task_no]      = @task_no
    )
        INSERT INTO [dbo].[Task_Tag] ([tag_id], [milestone_id], [task_no])
            VALUES (@tag_id, @milestone_id, @task_no);
END
