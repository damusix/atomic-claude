CREATE OR ALTER PROCEDURE [dbo].[sp_Project_Update]
    @project_id  INT,
    @name        NVARCHAR(255),
    @filepath    NVARCHAR(255) = N'',
    @git_repo    NVARCHAR(255) = N'',
    @main_branch NVARCHAR(255) = N'',
    @git_url     NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF @project_id = 0
    BEGIN
        RAISERROR('Project(0) is the sentinel and cannot be modified or deleted.', 16, 1);
        RETURN;
    END

    UPDATE [dbo].[Project]
        SET [name]        = @name,
            [filepath]    = @filepath,
            [git_repo]    = @git_repo,
            [main_branch] = @main_branch,
            [git_url]     = @git_url,
            [updated_at]  = SYSUTCDATETIME()
        WHERE [project_id] = @project_id;
END
