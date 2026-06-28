-- =============================================================================
-- Task_Dependency procedures
-- -----------------------------------------------------------------------------
-- sp_Task_Depend    — register a directed dependency between two tasks; rejects
--                     self-references, unknown verbs, and would-be cycles via
--                     fn_TaskDependencyWouldCycle. Idempotent on the PK
--                     (milestone_id, task_no, dep_milestone_id, dep_task_no).
-- sp_Task_Undepend  — silent delete; no error if the row was never there.
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Depend]
    @milestone_id     INT,
    @task_no          INT,
    @dep_milestone_id INT,
    @dep_task_no      INT,
    @dependency_verb  VARCHAR(32),
    @reason           NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    IF (@milestone_id = @dep_milestone_id AND @task_no = @dep_task_no)
    BEGIN
        RAISERROR('Task cannot depend on itself.', 16, 1);
        RETURN;
    END

    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[DependencyVerb] WHERE [dependency_verb] = @dependency_verb
    )
    BEGIN
        RAISERROR('Unknown dependency_verb: %s', 16, 1, @dependency_verb);
        RETURN;
    END

    IF [dbo].[fn_TaskDependencyWouldCycle](
            @milestone_id, @task_no, @dep_milestone_id, @dep_task_no
       ) = 1
    BEGIN
        RAISERROR('Adding this dependency would create a cycle.', 16, 1);
        RETURN;
    END

    -- Idempotent: silent no-op if dependency already exists
    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[Task_Dependency]
        WHERE [milestone_id]     = @milestone_id
          AND [task_no]          = @task_no
          AND [dep_milestone_id] = @dep_milestone_id
          AND [dep_task_no]      = @dep_task_no
    )
    BEGIN
        INSERT INTO [dbo].[Task_Dependency]
                ([milestone_id], [task_no],
                 [dep_milestone_id], [dep_task_no],
                 [dependency_verb], [reason])
            VALUES
                (@milestone_id, @task_no,
                 @dep_milestone_id, @dep_task_no,
                 @dependency_verb, @reason);
    END
END
