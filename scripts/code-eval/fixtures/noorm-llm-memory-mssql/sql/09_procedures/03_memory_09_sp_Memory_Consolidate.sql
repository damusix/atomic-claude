CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Consolidate]
    @canonical_memory_id INT,
    @duplicate_memory_id INT,
    @agent_id            INT,
    @reason              NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF @canonical_memory_id = @duplicate_memory_id
    BEGIN
        RAISERROR('Cannot consolidate a memory with itself.', 16, 1);
        RETURN;
    END

    IF NOT EXISTS (SELECT 1 FROM [dbo].[Memory] WHERE [memory_id] = @canonical_memory_id)
    BEGIN
        RAISERROR('Canonical Memory %d not found.', 16, 1, @canonical_memory_id);
        RETURN;
    END

    IF NOT EXISTS (SELECT 1 FROM [dbo].[Memory] WHERE [memory_id] = @duplicate_memory_id)
    BEGIN
        RAISERROR('Duplicate Memory %d not found.', 16, 1, @duplicate_memory_id);
        RETURN;
    END

    BEGIN TRANSACTION;
        -- 1. Record the supersedes relation (canonical supersedes duplicate)
        IF NOT EXISTS (
            SELECT 1 FROM [dbo].[Related_Memory]
            WHERE [memory_id] = @canonical_memory_id
              AND [related_memory_id] = @duplicate_memory_id
        )
        BEGIN
            INSERT INTO [dbo].[Related_Memory]
                    ([memory_id], [related_memory_id], [relation_verb], [reason])
                VALUES
                    (@canonical_memory_id, @duplicate_memory_id, 'supersedes', @reason);
        END

        -- 2. Re-point Memory_Tag (skip duplicates already attached to canonical)
        INSERT INTO [dbo].[Memory_Tag] ([tag_id], [memory_id])
        SELECT mt.[tag_id], @canonical_memory_id
        FROM [dbo].[Memory_Tag] mt
        WHERE mt.[memory_id] = @duplicate_memory_id
          AND NOT EXISTS (
              SELECT 1 FROM [dbo].[Memory_Tag] mt2
              WHERE mt2.[tag_id]    = mt.[tag_id]
                AND mt2.[memory_id] = @canonical_memory_id
          );
        DELETE FROM [dbo].[Memory_Tag] WHERE [memory_id] = @duplicate_memory_id;

        -- 3. Re-point Project_Memory (skip duplicates already attached to canonical)
        INSERT INTO [dbo].[Project_Memory] ([project_id], [memory_id])
        SELECT pm.[project_id], @canonical_memory_id
        FROM [dbo].[Project_Memory] pm
        WHERE pm.[memory_id] = @duplicate_memory_id
          AND NOT EXISTS (
              SELECT 1 FROM [dbo].[Project_Memory] pm2
              WHERE pm2.[project_id] = pm.[project_id]
                AND pm2.[memory_id]  = @canonical_memory_id
          );
        DELETE FROM [dbo].[Project_Memory] WHERE [memory_id] = @duplicate_memory_id;

        -- 4. Mark duplicate as 'superseded' through the state-machine gate
        EXEC [dbo].[sp_Memory_SetRelevance]
            @memory_id            = @duplicate_memory_id,
            @new_relevance_status = 'superseded',
            @agent_id             = @agent_id,
            @reason               = @reason;
    COMMIT TRANSACTION;
END
