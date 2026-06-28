CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Attach_Project]
    @memory_id  INT,
    @project_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Idempotent: skip if already attached
    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[Project_Memory]
        WHERE [project_id] = @project_id AND [memory_id] = @memory_id
    )
    BEGIN
        INSERT INTO [dbo].[Project_Memory] ([project_id], [memory_id])
            VALUES (@project_id, @memory_id);
    END
END
