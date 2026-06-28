CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Detach_Task]
    @tag_id       INT,
    @milestone_id INT,
    @task_no      INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Task_Tag]
        WHERE [tag_id]       = @tag_id
          AND [milestone_id] = @milestone_id
          AND [task_no]      = @task_no;
END
