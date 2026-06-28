CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Detach_Memory]
    @tag_id    INT,
    @memory_id INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Memory_Tag]
        WHERE [tag_id]    = @tag_id
          AND [memory_id] = @memory_id;
END
