-- =============================================================================
-- Tag procedures (inclusive subtype — one tag may attach to many entity types)
-- -----------------------------------------------------------------------------
-- sp_Tag_Create            — insert with unique-name guard
-- sp_Tag_Update            — modify metadata
-- sp_Tag_Delete            — hard delete; FK CASCADE on *_Tag wipes attachments
-- sp_Tag_Merge             — re-point all *_Tag rows from source to target,
--                            then hard-delete source
--
-- Idempotent attach / detach pairs (one per related entity type):
--   sp_Tag_Attach_Project   sp_Tag_Detach_Project
--   sp_Tag_Attach_Memory    sp_Tag_Detach_Memory
--   sp_Tag_Attach_Artifact  sp_Tag_Detach_Artifact
--   sp_Tag_Attach_Milestone sp_Tag_Detach_Milestone
--   sp_Tag_Attach_Task      sp_Tag_Detach_Task   (Task PK is composite)
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Create]
    @name          NVARCHAR(255),
    @description   NVARCHAR(255) = N'',
    @reason        NVARCHAR(255) = N'',
    @provenance_id INT           = 0,
    @agent_id      INT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    IF EXISTS (SELECT 1 FROM [dbo].[Tag] WHERE [name] = @name)
    BEGIN
        RAISERROR('Tag name %s already exists.', 16, 1, @name);
        RETURN;
    END

    INSERT INTO [dbo].[Tag]
            ([provenance_id], [agent_id], [name], [description], [reason])
        VALUES
            (@provenance_id, @agent_id, @name, @description, @reason);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [tag_id];
END
