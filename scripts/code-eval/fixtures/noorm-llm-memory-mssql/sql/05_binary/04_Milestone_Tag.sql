CREATE TABLE [dbo].[Milestone_Tag] (
    [tag_id]       INT NOT NULL,
    [milestone_id] INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Milestone_Tag] PRIMARY KEY ([tag_id], [milestone_id]),
    -- CASCADE on Tag side: sp_Tag_Delete relies on this to wipe attachments.
    CONSTRAINT [FK_Milestone_Tag_Tag] FOREIGN KEY ([tag_id])
        REFERENCES [dbo].[Tag] ([tag_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Milestone side: Milestone delete is soft first; hard delete
    -- via sp_Cleanup handles the join row removal. Avoids multi-cascade-path
    -- collisions with Task_Tag (which also reaches Milestone via Task).
    CONSTRAINT [FK_Milestone_Tag_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
