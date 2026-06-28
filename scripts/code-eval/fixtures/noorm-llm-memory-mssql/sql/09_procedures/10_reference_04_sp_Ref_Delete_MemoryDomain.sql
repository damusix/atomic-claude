CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Delete_MemoryDomain]
    @domain VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF EXISTS (SELECT 1 FROM [dbo].[Memory] WHERE [domain] = @domain)
    BEGIN
        RAISERROR('Cannot delete MemoryDomain %s — it is in use by one or more memories.',
                  16, 1, @domain);
        RETURN;
    END

    DELETE FROM [dbo].[MemoryDomain] WHERE [domain] = @domain;
END
