CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Update]
    @tag_id      INT,
    @name        NVARCHAR(255),
    @description NVARCHAR(255) = N'',
    @reason      NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE [dbo].[Tag]
        SET [name]        = @name,
            [description] = @description,
            [reason]      = @reason,
            [updated_at]  = SYSUTCDATETIME()
        WHERE [tag_id] = @tag_id;
END
