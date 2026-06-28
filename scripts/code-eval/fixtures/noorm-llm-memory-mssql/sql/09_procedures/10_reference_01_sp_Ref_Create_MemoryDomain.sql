-- =============================================================================
-- Reference table management procedures
-- -----------------------------------------------------------------------------
-- Add or remove rows in the small enum-like reference tables. Deletes are
-- guarded against in-use values so the FK contract on the dependent tables
-- can never be silently broken.
--
-- Create variants are idempotent (silent no-op on duplicate). Delete variants
-- raise an error if any dependent row still references the value.
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Create_MemoryDomain]
    @domain VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (SELECT 1 FROM [dbo].[MemoryDomain] WHERE [domain] = @domain)
    BEGIN
        INSERT INTO [dbo].[MemoryDomain] ([domain]) VALUES (@domain);
    END
END
