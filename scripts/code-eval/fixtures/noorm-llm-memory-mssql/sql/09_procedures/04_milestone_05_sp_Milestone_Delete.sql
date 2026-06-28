CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Delete]
    @milestone_id INT,
    @agent_id     INT,
    @reason       NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    -- Soft-delete every Note attached to this Milestone (Milestone_Note +
    -- Task_Note rows where the parent Task lives under this milestone).
    -- Each call validates the per-note state machine; skip rows that would be
    -- rejected (already deleted or no allowed transition).
    DECLARE @note_id INT;
    DECLARE @cur_rs  VARCHAR(32);

    DECLARE note_cur CURSOR LOCAL FAST_FORWARD FOR
        SELECT mn.[note_id]
        FROM [dbo].[Milestone_Note] mn
        WHERE mn.[milestone_id] = @milestone_id
        UNION
        SELECT tn.[note_id]
        FROM [dbo].[Task_Note] tn
        WHERE tn.[milestone_id] = @milestone_id;

    OPEN note_cur;
    FETCH NEXT FROM note_cur INTO @note_id;
    WHILE @@FETCH_STATUS = 0
    BEGIN
        SELECT @cur_rs = [relevance_status]
        FROM [dbo].[Note]
        WHERE [note_id] = @note_id;

        IF @cur_rs <> 'deleted'
           AND [dbo].[fn_IsRelevanceTransitionAllowed](@cur_rs, 'deleted') = 1
        BEGIN
            EXEC [dbo].[sp_Note_Delete]
                @note_id  = @note_id,
                @agent_id = 0,
                @reason   = N'Cascade soft-delete from sp_Milestone_Delete';
        END

        FETCH NEXT FROM note_cur INTO @note_id;
    END
    CLOSE note_cur;
    DEALLOCATE note_cur;

    -- Mark the Milestone itself as deleted via the state machine.
    EXEC [dbo].[sp_Milestone_SetRelevance]
        @milestone_id         = @milestone_id,
        @new_relevance_status = 'deleted',
        @agent_id             = @agent_id,
        @reason               = @reason;
END
