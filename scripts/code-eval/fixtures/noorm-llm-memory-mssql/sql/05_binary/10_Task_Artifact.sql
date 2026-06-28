CREATE TABLE [dbo].[Task_Artifact] (
    [milestone_id] INT NOT NULL,
    [task_no]      INT NOT NULL,
    [artifact_id]  INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task_Artifact] PRIMARY KEY ([milestone_id], [task_no], [artifact_id]),
    -- CASCADE on Task side: Task hard-delete (via Milestone cascade in sp_Cleanup)
    -- removes its artifact attachments. Task owns the lifecycle of this row.
    CONSTRAINT [FK_Task_Artifact_Task] FOREIGN KEY ([milestone_id], [task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Artifact side: avoids the multi-cascade-path conflict with
    -- Milestone_Artifact (both join tables reach Artifact). Cleanup must DELETE
    -- this row explicitly when an Artifact is hard-deleted.
    CONSTRAINT [FK_Task_Artifact_Artifact] FOREIGN KEY ([artifact_id])
        REFERENCES [dbo].[Artifact] ([artifact_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
