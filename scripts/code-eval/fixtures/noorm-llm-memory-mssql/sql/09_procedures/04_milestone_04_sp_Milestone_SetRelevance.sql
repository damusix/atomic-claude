CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_SetRelevance]
    @milestone_id         INT,
    @new_relevance_status VARCHAR(32),
    @agent_id             INT,
    @reason               NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    DECLARE @from_status   VARCHAR(32);
    DECLARE @transition_id INT;

    SELECT @from_status = [relevance_status]
    FROM [dbo].[Milestone]
    WHERE [milestone_id] = @milestone_id;

    IF @from_status IS NULL
    BEGIN
        RAISERROR('Milestone %d not found.', 16, 1, @milestone_id);
        RETURN;
    END

    IF [dbo].[fn_IsRelevanceTransitionAllowed](@from_status, @new_relevance_status) = 0
    BEGIN
        RAISERROR('Relevance transition not allowed: %s -> %s',
                  16, 1, @from_status, @new_relevance_status);
        RETURN;
    END

    BEGIN TRANSACTION;
        UPDATE [dbo].[Milestone]
            SET [relevance_status] = @new_relevance_status,
                [updated_at]       = SYSUTCDATETIME()
            WHERE [milestone_id] = @milestone_id;

        INSERT INTO [dbo].[StateTransition]
                ([state_transition_type], [agent_id], [from_status],
                 [to_status], [reason], [occurred_at])
            VALUES
                ('milestone-relevance', @agent_id, @from_status,
                 @new_relevance_status, @reason, SYSUTCDATETIME());
        SET @transition_id = SCOPE_IDENTITY();

        INSERT INTO [dbo].[Milestone_StateTransition]
                ([transition_id], [milestone_id])
            VALUES
                (@transition_id, @milestone_id);
    COMMIT TRANSACTION;
END
