CREATE OR ALTER PROCEDURE [dbo].[sp_Task_Bulk_Depend]
    @Deps [dbo].[TaskDependencyInput] READONLY
AS
BEGIN
    SET NOCOUNT ON;

    -- Reject self-references in the input batch. One bad row aborts the batch.
    IF EXISTS (
        SELECT 1 FROM @Deps
        WHERE [milestone_id] = [dep_milestone_id] AND [task_no] = [dep_task_no]
    )
    BEGIN
        RAISERROR('Self-referential dependency in TVP rejected.', 16, 1);
        RETURN;
    END

    -- Reject any row whose dependency_verb is unknown.
    IF EXISTS (
        SELECT 1
        FROM @Deps d
        WHERE NOT EXISTS (
            SELECT 1 FROM [dbo].[DependencyVerb] dv
            WHERE dv.[dependency_verb] = d.[dependency_verb]
        )
    )
    BEGIN
        RAISERROR('Unknown dependency_verb in TVP.', 16, 1);
        RETURN;
    END

    -- Reject the entire batch if any row would form a cycle. The cycle check
    -- runs against the CURRENT graph (not the would-be-updated one), so a
    -- batch that introduces cycles only when combined is not detected here.
    -- Single-row sp_Task_Depend remains the safe path for cycle-sensitive work.
    IF EXISTS (
        SELECT 1
        FROM @Deps d
        WHERE [dbo].[fn_TaskDependencyWouldCycle](
                d.[milestone_id], d.[task_no],
                d.[dep_milestone_id], d.[dep_task_no]
              ) = 1
    )
    BEGIN
        RAISERROR('Bulk dependency batch would create a cycle.', 16, 1);
        RETURN;
    END

    -- Idempotent: skip rows that already exist on the PK.
    INSERT INTO [dbo].[Task_Dependency]
            ([milestone_id], [task_no],
             [dep_milestone_id], [dep_task_no],
             [dependency_verb], [reason])
    SELECT d.[milestone_id], d.[task_no],
           d.[dep_milestone_id], d.[dep_task_no],
           d.[dependency_verb], d.[reason]
    FROM @Deps d
    WHERE NOT EXISTS (
        SELECT 1 FROM [dbo].[Task_Dependency] td
        WHERE td.[milestone_id]     = d.[milestone_id]
          AND td.[task_no]          = d.[task_no]
          AND td.[dep_milestone_id] = d.[dep_milestone_id]
          AND td.[dep_task_no]      = d.[dep_task_no]
    );
END
