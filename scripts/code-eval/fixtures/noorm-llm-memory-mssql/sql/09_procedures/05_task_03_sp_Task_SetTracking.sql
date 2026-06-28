CREATE OR ALTER PROCEDURE [dbo].[sp_Task_SetTracking]
    @milestone_id        INT,
    @task_no             INT,
    @new_tracking_status VARCHAR(32),
    @agent_id            INT,
    @reason              NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    DECLARE @from_status   VARCHAR(32);
    DECLARE @transition_id INT;

    SELECT @from_status = [tracking_status]
    FROM [dbo].[Task]
    WHERE [milestone_id] = @milestone_id
      AND [task_no]      = @task_no;

    IF @from_status IS NULL
    BEGIN
        RAISERROR('Task (%d, %d) not found.', 16, 1, @milestone_id, @task_no);
        RETURN;
    END

    IF [dbo].[fn_IsTrackingTransitionAllowed](@from_status, @new_tracking_status) = 0
    BEGIN
        RAISERROR('Tracking transition not allowed: %s -> %s',
                  16, 1, @from_status, @new_tracking_status);
        RETURN;
    END

    BEGIN TRANSACTION;
        UPDATE [dbo].[Task]
            SET [tracking_status] = @new_tracking_status,
                [updated_at]      = SYSUTCDATETIME()
            WHERE [milestone_id] = @milestone_id
              AND [task_no]      = @task_no;

        INSERT INTO [dbo].[StateTransition]
                ([state_transition_type], [agent_id], [from_status],
                 [to_status], [reason], [occurred_at])
            VALUES
                ('task-tracking', @agent_id, @from_status,
                 @new_tracking_status, @reason, SYSUTCDATETIME());
        SET @transition_id = SCOPE_IDENTITY();

        INSERT INTO [dbo].[Task_StateTransition]
                ([transition_id], [milestone_id], [task_no])
            VALUES
                (@transition_id, @milestone_id, @task_no);
    COMMIT TRANSACTION;
END
