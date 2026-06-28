-- =============================================================================
-- Project procedures
-- -----------------------------------------------------------------------------
-- sp_Project_Create — insert and return new project_id
-- sp_Project_Update — modify metadata (sentinel-protected)
-- sp_Project_Delete — reassign provenance to 0, soft-delete attached Notes,
--                     then hard-delete the Project row
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Project_Create]
    @name        NVARCHAR(255),
    @filepath    NVARCHAR(255) = N'',
    @git_repo    NVARCHAR(255) = N'',
    @main_branch NVARCHAR(255) = N'',
    @git_url     NVARCHAR(255) = N'',
    @agent_id    INT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    INSERT INTO [dbo].[Project]
            ([agent_id], [name], [filepath], [git_repo], [main_branch], [git_url])
        VALUES
            (@agent_id, @name, @filepath, @git_repo, @main_branch, @git_url);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [project_id];
END
