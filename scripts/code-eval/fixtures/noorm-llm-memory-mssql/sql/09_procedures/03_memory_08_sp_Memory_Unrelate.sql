CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Unrelate]
    @memory_id         INT,
    @related_memory_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Silent on missing row: detach is idempotent.
    DELETE FROM [dbo].[Related_Memory]
        WHERE [memory_id] = @memory_id
          AND [related_memory_id] = @related_memory_id;
END
