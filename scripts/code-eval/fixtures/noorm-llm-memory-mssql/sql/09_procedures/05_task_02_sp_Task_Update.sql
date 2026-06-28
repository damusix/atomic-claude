CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Update]
    @milestone_id INT,
    @task_no      INT,
    @title        NVARCHAR(255),
    @content      NVARCHAR(MAX) = N'',
    @reason       NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE [dbo].[Task]
        SET [title]      = @title,
            [content]    = @content,
            [reason]     = @reason,
            [updated_at] = SYSUTCDATETIME()
        WHERE [milestone_id] = @milestone_id
          AND [task_no]      = @task_no;
END
