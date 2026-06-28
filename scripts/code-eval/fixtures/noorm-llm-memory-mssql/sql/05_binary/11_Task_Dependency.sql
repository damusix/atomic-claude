CREATE TABLE [dbo].[Task_Dependency] (
    [milestone_id]     INT NOT NULL,
    [task_no]          INT NOT NULL,
    [dep_milestone_id] INT NOT NULL,
    [dep_task_no]      INT NOT NULL,
    [dependency_verb]  VARCHAR(32) NOT NULL,
    [reason]           NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task_Dependency] PRIMARY KEY ([milestone_id], [task_no], [dep_milestone_id], [dep_task_no]),
    -- CASCADE on the dependent task side: when the task that HAS the dependency
    -- is hard-deleted, its outgoing dependency rows die with it.
    CONSTRAINT [FK_Task_Dependency_Task] FOREIGN KEY ([milestone_id], [task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on the depended-upon task side: MSSQL forbids two cascade paths
    -- to the same table (Task). sp_Cleanup must DELETE rows referencing a
    -- to-be-deleted task before the task entity itself is removed.
    CONSTRAINT [FK_Task_Dependency_Dep] FOREIGN KEY ([dep_milestone_id], [dep_task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- NO ACTION on verb: sp_Ref_Delete_DependencyVerb guards the reference
    -- table from removal while in use.
    CONSTRAINT [FK_Task_Dependency_Verb] FOREIGN KEY ([dependency_verb])
        REFERENCES [dbo].[DependencyVerb] ([dependency_verb])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
