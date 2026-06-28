CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Detach_Project]
    @memory_id  INT,
    @project_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Silent on missing row: detach is idempotent.
    DELETE FROM [dbo].[Project_Memory]
        WHERE [project_id] = @project_id
          AND [memory_id]  = @memory_id;
END
