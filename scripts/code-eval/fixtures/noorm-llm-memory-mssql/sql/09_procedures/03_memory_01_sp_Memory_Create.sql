-- =============================================================================
-- Memory procedures
-- -----------------------------------------------------------------------------
-- sp_Memory_Create          — insert (active, last_accessed=now, count=0)
-- sp_Memory_Update          — modify content/domain/category/booleans (no status)
-- sp_Memory_SetRelevance    — gated state-machine transition + audit row
-- sp_Memory_Delete          — wraps SetRelevance to 'deleted'
-- sp_Memory_Restore         — wraps SetRelevance to 'active'
-- sp_Memory_Touch           — bump last_accessed_at + access_count (no updated_at)
-- sp_Memory_Relate          — insert Related_Memory (idempotent, validated verb)
-- sp_Memory_Unrelate        — delete Related_Memory (silent on miss)
-- sp_Memory_Consolidate     — merge duplicate into canonical (tags, projects,
--                             then mark duplicate as 'superseded')
-- sp_Memory_Attach_Project  — idempotent insert into Project_Memory
-- sp_Memory_Detach_Project  — silent delete from Project_Memory
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Create]
    @content           NVARCHAR(MAX),
    @domain            VARCHAR(32),
    @category          VARCHAR(32),
    @reason            NVARCHAR(255) = N'',
    @provenance_id     INT           = 0,
    @agent_id          INT           = 0,
    @was_inferred      BIT           = 0,
    @was_observed      BIT           = 0,
    @was_evidenced     BIT           = 0,
    @was_user_provided BIT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

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

    INSERT INTO [dbo].[Memory]
            ([domain], [category], [relevance_status], [provenance_id], [agent_id],
             [content], [reason],
             [was_inferred], [was_observed], [was_evidenced], [was_user_provided],
             [last_accessed_at], [access_count])
        VALUES
            (@domain, @category, 'active', @provenance_id, @agent_id,
             @content, @reason,
             @was_inferred, @was_observed, @was_evidenced, @was_user_provided,
             SYSUTCDATETIME(), 0);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [memory_id];
END
