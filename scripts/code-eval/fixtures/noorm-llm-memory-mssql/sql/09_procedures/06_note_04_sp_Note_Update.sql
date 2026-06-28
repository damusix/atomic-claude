CREATE OR ALTER PROCEDURE [dbo].[sp_Note_Update]
    @note_id INT,
    @content NVARCHAR(MAX),
    @reason  NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    -- note_type and subtype attachment are immutable. Use SetRelevance for status.
    UPDATE [dbo].[Note]
        SET [content]    = @content,
            [reason]     = @reason,
            [updated_at] = SYSUTCDATETIME()
        WHERE [note_id] = @note_id;
END
