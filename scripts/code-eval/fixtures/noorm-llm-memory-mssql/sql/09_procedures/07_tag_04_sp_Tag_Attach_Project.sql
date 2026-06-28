-- ---------------------------------------------------------------------------
-- Attach / Detach — Project
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Attach_Project]
    @tag_id     INT,
    @project_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Project_Tag]
        WHERE [tag_id]     = @tag_id
          AND [project_id] = @project_id
    )
        INSERT INTO [dbo].[Project_Tag] ([tag_id], [project_id])
            VALUES (@tag_id, @project_id);
END
