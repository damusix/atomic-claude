CREATE TABLE [dbo].[Milestone_Artifact] (
    [milestone_id] INT NOT NULL,
    [artifact_id]  INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Milestone_Artifact] PRIMARY KEY ([milestone_id], [artifact_id]),
    -- CASCADE on Milestone side: Milestone hard-delete via sp_Cleanup removes
    -- its artifact attachments. Milestone owns the lifecycle of this row.
    CONSTRAINT [FK_Milestone_Artifact_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Artifact side: Artifact delete is soft first; hard delete
    -- via sp_Cleanup must DELETE this join row explicitly to avoid the
    -- multi-cascade-path conflict with Task_Artifact reaching the same Artifact.
    CONSTRAINT [FK_Milestone_Artifact_Artifact] FOREIGN KEY ([artifact_id])
        REFERENCES [dbo].[Artifact] ([artifact_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
