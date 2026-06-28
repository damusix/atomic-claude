CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Delete]
    @tag_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Hard delete; FK ON DELETE CASCADE on every *_Tag table removes attachments.
    DELETE FROM [dbo].[Tag] WHERE [tag_id] = @tag_id;
END
