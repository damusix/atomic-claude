CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Create_MemoryCategory]
    @category VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (SELECT 1 FROM [dbo].[MemoryCategory] WHERE [category] = @category)
    BEGIN
        INSERT INTO [dbo].[MemoryCategory] ([category]) VALUES (@category);
    END
END
