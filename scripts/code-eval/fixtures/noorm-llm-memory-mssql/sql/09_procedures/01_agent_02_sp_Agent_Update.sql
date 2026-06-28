CREATE OR ALTER PROCEDURE [dbo].[sp_Agent_Update]
    @agent_id    INT,
    @name        NVARCHAR(255),
    @description NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF @agent_id = 0
    BEGIN
        RAISERROR('Agent(0) is the sentinel and cannot be modified or deleted.', 16, 1);
        RETURN;
    END

    UPDATE [dbo].[Agent]
        SET [name]        = @name,
            [description] = @description,
            [updated_at]  = SYSUTCDATETIME()
        WHERE [agent_id] = @agent_id;
END
