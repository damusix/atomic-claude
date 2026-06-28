CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Relate]
    @memory_id         INT,
    @related_memory_id INT,
    @relation_verb     VARCHAR(32),
    @reason            NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF @memory_id = @related_memory_id
    BEGIN
        RAISERROR('Cannot relate a memory to itself.', 16, 1);
        RETURN;
    END

    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[MemoryRelationVerb] WHERE [verb_forward] = @relation_verb
    )
    BEGIN
        RAISERROR('Unknown relation_verb: %s', 16, 1, @relation_verb);
        RETURN;
    END

    -- Idempotent: skip if relation already exists in this direction
    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[Related_Memory]
        WHERE [memory_id] = @memory_id AND [related_memory_id] = @related_memory_id
    )
    BEGIN
        INSERT INTO [dbo].[Related_Memory]
                ([memory_id], [related_memory_id], [relation_verb], [reason])
            VALUES
                (@memory_id, @related_memory_id, @relation_verb, @reason);
    END
END
