CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Touch]
    @memory_id INT,
    @agent_id  INT = 0
AS
BEGIN
    SET NOCOUNT ON;

    -- Touch is a read-side signal: bump last_accessed_at and access_count
    -- only. updated_at is intentionally NOT modified.
    UPDATE [dbo].[Memory]
        SET [last_accessed_at] = SYSUTCDATETIME(),
            [access_count]     = [access_count] + 1
        WHERE [memory_id] = @memory_id;
END
