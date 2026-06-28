CREATE TABLE [dbo].[Project_Milestone] (
    [project_id]   INT NOT NULL,
    [milestone_id] INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Project_Milestone] PRIMARY KEY ([project_id], [milestone_id]),
    -- NO ACTION on Project side: sp_Project_Delete explicitly clears join rows
    -- and reassigns provenance to sentinel 0; the sentinel itself is undeletable.
    CONSTRAINT [FK_Project_Milestone_Project] FOREIGN KEY ([project_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- CASCADE on Milestone side: Milestone hard-delete via sp_Cleanup must
    -- remove this association row automatically. Milestone owns the lifecycle.
    CONSTRAINT [FK_Project_Milestone_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE CASCADE ON UPDATE NO ACTION
);
