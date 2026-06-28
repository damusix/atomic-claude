CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Detach_Project]
    @tag_id     INT,
    @project_id INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Project_Tag]
        WHERE [tag_id]     = @tag_id
          AND [project_id] = @project_id;
END
