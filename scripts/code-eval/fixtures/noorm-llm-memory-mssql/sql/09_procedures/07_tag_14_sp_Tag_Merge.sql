-- ---------------------------------------------------------------------------
-- Tag merge — collapse source into target across every *_Tag table
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Merge]
    @source_tag_id INT,
    @target_tag_id INT,
    @agent_id      INT,
    @reason        NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF @source_tag_id = @target_tag_id
    BEGIN
        RAISERROR('Cannot merge a tag into itself.', 16, 1);
        RETURN;
    END

    IF NOT EXISTS (SELECT 1 FROM [dbo].[Tag] WHERE [tag_id] = @source_tag_id)
    BEGIN
        RAISERROR('Source tag %d does not exist.', 16, 1, @source_tag_id);
        RETURN;
    END

    IF NOT EXISTS (SELECT 1 FROM [dbo].[Tag] WHERE [tag_id] = @target_tag_id)
    BEGIN
        RAISERROR('Target tag %d does not exist.', 16, 1, @target_tag_id);
        RETURN;
    END

    BEGIN TRANSACTION;
        -- Project_Tag
        INSERT INTO [dbo].[Project_Tag] ([tag_id], [project_id])
        SELECT @target_tag_id, src.[project_id]
        FROM [dbo].[Project_Tag] src
        WHERE src.[tag_id] = @source_tag_id
          AND NOT EXISTS (
              SELECT 1
              FROM [dbo].[Project_Tag] dst
              WHERE dst.[tag_id]     = @target_tag_id
                AND dst.[project_id] = src.[project_id]
          );
        DELETE FROM [dbo].[Project_Tag] WHERE [tag_id] = @source_tag_id;

        -- Memory_Tag
        INSERT INTO [dbo].[Memory_Tag] ([tag_id], [memory_id])
        SELECT @target_tag_id, src.[memory_id]
        FROM [dbo].[Memory_Tag] src
        WHERE src.[tag_id] = @source_tag_id
          AND NOT EXISTS (
              SELECT 1
              FROM [dbo].[Memory_Tag] dst
              WHERE dst.[tag_id]    = @target_tag_id
                AND dst.[memory_id] = src.[memory_id]
          );
        DELETE FROM [dbo].[Memory_Tag] WHERE [tag_id] = @source_tag_id;

        -- Artifact_Tag
        INSERT INTO [dbo].[Artifact_Tag] ([tag_id], [artifact_id])
        SELECT @target_tag_id, src.[artifact_id]
        FROM [dbo].[Artifact_Tag] src
        WHERE src.[tag_id] = @source_tag_id
          AND NOT EXISTS (
              SELECT 1
              FROM [dbo].[Artifact_Tag] dst
              WHERE dst.[tag_id]      = @target_tag_id
                AND dst.[artifact_id] = src.[artifact_id]
          );
        DELETE FROM [dbo].[Artifact_Tag] WHERE [tag_id] = @source_tag_id;

        -- Milestone_Tag
        INSERT INTO [dbo].[Milestone_Tag] ([tag_id], [milestone_id])
        SELECT @target_tag_id, src.[milestone_id]
        FROM [dbo].[Milestone_Tag] src
        WHERE src.[tag_id] = @source_tag_id
          AND NOT EXISTS (
              SELECT 1
              FROM [dbo].[Milestone_Tag] dst
              WHERE dst.[tag_id]       = @target_tag_id
                AND dst.[milestone_id] = src.[milestone_id]
          );
        DELETE FROM [dbo].[Milestone_Tag] WHERE [tag_id] = @source_tag_id;

        -- Task_Tag (composite)
        INSERT INTO [dbo].[Task_Tag] ([tag_id], [milestone_id], [task_no])
        SELECT @target_tag_id, src.[milestone_id], src.[task_no]
        FROM [dbo].[Task_Tag] src
        WHERE src.[tag_id] = @source_tag_id
          AND NOT EXISTS (
              SELECT 1
              FROM [dbo].[Task_Tag] dst
              WHERE dst.[tag_id]       = @target_tag_id
                AND dst.[milestone_id] = src.[milestone_id]
                AND dst.[task_no]      = src.[task_no]
          );
        DELETE FROM [dbo].[Task_Tag] WHERE [tag_id] = @source_tag_id;

        -- Hard delete the (now-empty) source tag. CASCADE wipes any leftover rows.
        DELETE FROM [dbo].[Tag] WHERE [tag_id] = @source_tag_id;
    COMMIT TRANSACTION;
END
