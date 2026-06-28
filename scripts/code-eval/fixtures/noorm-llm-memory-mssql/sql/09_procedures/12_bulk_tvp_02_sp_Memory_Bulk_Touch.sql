CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Bulk_Touch]
    @MemoryIds [dbo].[MemoryIdSet] READONLY
AS
BEGIN
    SET NOCOUNT ON;

    -- Touch is a read-side signal: bump last_accessed_at + access_count.
    -- updated_at is intentionally NOT modified (matches sp_Memory_Touch).
    UPDATE m
        SET [last_accessed_at] = SYSUTCDATETIME(),
            [access_count]     = [access_count] + 1
        FROM [dbo].[Memory] m
        INNER JOIN @MemoryIds ids ON ids.[memory_id] = m.[memory_id];
END
