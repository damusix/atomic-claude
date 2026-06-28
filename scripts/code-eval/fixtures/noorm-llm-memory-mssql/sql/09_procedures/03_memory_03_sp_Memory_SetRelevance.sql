CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_SetRelevance]
    @memory_id            INT,
    @new_relevance_status VARCHAR(32),
    @agent_id             INT,
    @reason               NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @from_status   VARCHAR(32);
    DECLARE @transition_id INT;

    SELECT @from_status = [relevance_status]
    FROM [dbo].[Memory]
    WHERE [memory_id] = @memory_id;

    IF @from_status IS NULL
    BEGIN
        RAISERROR('Memory %d not found.', 16, 1, @memory_id);
        RETURN;
    END

    IF [dbo].[fn_IsRelevanceTransitionAllowed](@from_status, @new_relevance_status) = 0
    BEGIN
        RAISERROR('Relevance transition not allowed: %s -> %s',
                  16, 1, @from_status, @new_relevance_status);
        RETURN;
    END

    BEGIN TRANSACTION;
        UPDATE [dbo].[Memory]
            SET [relevance_status] = @new_relevance_status,
                [updated_at]       = SYSUTCDATETIME()
            WHERE [memory_id] = @memory_id;

        INSERT INTO [dbo].[StateTransition]
                ([state_transition_type], [agent_id],
                 [from_status], [to_status], [reason], [occurred_at])
            VALUES
                ('memory-relevance', @agent_id,
                 @from_status, @new_relevance_status, @reason, SYSUTCDATETIME());
        SET @transition_id = SCOPE_IDENTITY();

        INSERT INTO [dbo].[Memory_StateTransition] ([transition_id], [memory_id])
            VALUES (@transition_id, @memory_id);
    COMMIT TRANSACTION;
END
