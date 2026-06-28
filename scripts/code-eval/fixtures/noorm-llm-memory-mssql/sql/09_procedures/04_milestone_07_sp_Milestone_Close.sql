CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Close]
    @milestone_id INT,
    @agent_id     INT,
    @reason       NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    BEGIN TRANSACTION;
        -- Each underlying call validates its own transition; if the milestone
        -- is already done/superseded, the inner RAISERROR rolls back the batch.
        EXEC [dbo].[sp_Milestone_SetTracking]
            @milestone_id        = @milestone_id,
            @new_tracking_status = 'done',
            @agent_id            = @agent_id,
            @reason              = @reason;

        EXEC [dbo].[sp_Milestone_SetRelevance]
            @milestone_id         = @milestone_id,
            @new_relevance_status = 'superseded',
            @agent_id             = @agent_id,
            @reason               = @reason;

        DECLARE @milestone_id_v INT = @milestone_id;
        DECLARE @task_no        INT;

        DECLARE task_cur CURSOR LOCAL FAST_FORWARD FOR
            SELECT [task_no]
            FROM [dbo].[Task]
            WHERE [milestone_id] = @milestone_id_v
              AND [dbo].[fn_IsOpen]([tracking_status]) = 1;

        OPEN task_cur;
        FETCH NEXT FROM task_cur INTO @task_no;
        WHILE @@FETCH_STATUS = 0
        BEGIN
            EXEC [dbo].[sp_Task_SetTracking]
                @milestone_id        = @milestone_id_v,
                @task_no             = @task_no,
                @new_tracking_status = 'abandoned',
                @agent_id            = @agent_id,
                @reason              = @reason;

            FETCH NEXT FROM task_cur INTO @task_no;
        END
        CLOSE task_cur;
        DEALLOCATE task_cur;
    COMMIT TRANSACTION;
END
