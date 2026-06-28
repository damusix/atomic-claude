-- ---------------------------------------------------------------------------
-- Attach / Detach — Memory
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Attach_Memory]
    @tag_id    INT,
    @memory_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Memory_Tag]
        WHERE [tag_id]    = @tag_id
          AND [memory_id] = @memory_id
    )
        INSERT INTO [dbo].[Memory_Tag] ([tag_id], [memory_id])
            VALUES (@tag_id, @memory_id);
END
