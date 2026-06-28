CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Update]
    @memory_id         INT,
    @content           NVARCHAR(MAX),
    @domain            VARCHAR(32),
    @category          VARCHAR(32),
    @reason            NVARCHAR(255) = N'',
    @was_inferred      BIT           = 0,
    @was_observed      BIT           = 0,
    @was_evidenced     BIT           = 0,
    @was_user_provided BIT           = 0
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (SELECT 1 FROM [dbo].[MemoryDomain] WHERE [domain] = @domain)
    BEGIN
        RAISERROR('Unknown MemoryDomain: %s', 16, 1, @domain);
        RETURN;
    END

    IF NOT EXISTS (SELECT 1 FROM [dbo].[MemoryCategory] WHERE [category] = @category)
    BEGIN
        RAISERROR('Unknown MemoryCategory: %s', 16, 1, @category);
        RETURN;
    END

    UPDATE [dbo].[Memory]
        SET [content]           = @content,
            [domain]            = @domain,
            [category]          = @category,
            [reason]            = @reason,
            [was_inferred]      = @was_inferred,
            [was_observed]      = @was_observed,
            [was_evidenced]     = @was_evidenced,
            [was_user_provided] = @was_user_provided,
            [updated_at]        = SYSUTCDATETIME()
        WHERE [memory_id] = @memory_id;
END
