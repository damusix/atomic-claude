CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Update]
    @artifact_id INT,
    @title       NVARCHAR(255),
    @description NVARCHAR(255) = N'',
    @filepath    NVARCHAR(255) = N'',
    @reason      NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    UPDATE [dbo].[Artifact]
        SET [title]       = @title,
            [description] = @description,
            [filepath]    = @filepath,
            [reason]      = @reason,
            [updated_at]  = SYSUTCDATETIME()
        WHERE [artifact_id] = @artifact_id;
END
