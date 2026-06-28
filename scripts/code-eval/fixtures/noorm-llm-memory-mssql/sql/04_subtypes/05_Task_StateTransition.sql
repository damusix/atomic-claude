CREATE TABLE [dbo].[Task_StateTransition] (
    [transition_id] INT NOT NULL,
    [milestone_id]  INT NOT NULL,
    [task_no]       INT NOT NULL,
    [created_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task_StateTransition] PRIMARY KEY ([transition_id]),
    -- CASCADE from basetype: deleting the StateTransition removes the subtype row.
    CONSTRAINT [FK_Task_StateTransition_StateTransition] FOREIGN KEY ([transition_id])
        REFERENCES [dbo].[StateTransition] ([transition_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Task hard-delete (via parent Milestone's CASCADE) and
    -- sp_Cleanup's expired-transition pass jointly handle removal. A direct
    -- cascade from Task here would form a second path through Milestone and
    -- be rejected by MSSQL's multi-cascade-path rule.
    CONSTRAINT [FK_Task_StateTransition_Task] FOREIGN KEY ([milestone_id], [task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces StateTransition.state_transition_type =
    -- 'task-tracking' without a trigger. Calls fn_StateTransitionIsOfType
    -- (declared in sql/03_validators/).
    CONSTRAINT [CK_Task_StateTransition_Type]
        CHECK ([dbo].[fn_StateTransitionIsOfType]([transition_id], 'task-tracking') = 1)
);
