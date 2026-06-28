CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Delete]
    @milestone_id INT,
    @task_no      INT,
    @agent_id     INT,
    @reason       NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    -- Soft-delete every Note attached to this Task. Skip rows the relevance
    -- state machine would refuse (already deleted, or no allowed edge).
    DECLARE @note_id INT;
    DECLARE @cur_rs  VARCHAR(32);

    DECLARE note_cur CURSOR LOCAL FAST_FORWARD FOR
        SELECT tn.[note_id]
        FROM [dbo].[Task_Note] tn
        WHERE tn.[milestone_id] = @milestone_id
          AND tn.[task_no]      = @task_no;

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
                @reason   = N'Cascade soft-delete from sp_Task_Delete';
        END

        FETCH NEXT FROM note_cur INTO @note_id;
    END
    CLOSE note_cur;
    DEALLOCATE note_cur;

    -- Tasks have no relevance_status of their own; abandoned is the soft delete.
    EXEC [dbo].[sp_Task_SetTracking]
        @milestone_id        = @milestone_id,
        @task_no             = @task_no,
        @new_tracking_status = 'abandoned',
        @agent_id            = @agent_id,
        @reason              = @reason;
END
