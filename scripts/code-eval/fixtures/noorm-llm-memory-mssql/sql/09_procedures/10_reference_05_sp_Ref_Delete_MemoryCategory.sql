CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Delete_MemoryCategory]
    @category VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF EXISTS (SELECT 1 FROM [dbo].[Memory] WHERE [category] = @category)
    BEGIN
        RAISERROR('Cannot delete MemoryCategory %s — it is in use by one or more memories.',
                  16, 1, @category);
        RETURN;
    END

    DELETE FROM [dbo].[MemoryCategory] WHERE [category] = @category;
END
