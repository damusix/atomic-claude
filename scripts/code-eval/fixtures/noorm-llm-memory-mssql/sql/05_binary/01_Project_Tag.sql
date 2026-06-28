CREATE TABLE [dbo].[Project_Tag] (
    [tag_id]     INT NOT NULL,
    [project_id] INT NOT NULL,
    [created_at] DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Project_Tag] PRIMARY KEY ([tag_id], [project_id]),
    -- CASCADE on Tag side: sp_Tag_Delete relies on this to wipe attachments.
    CONSTRAINT [FK_Project_Tag_Tag] FOREIGN KEY ([tag_id])
        REFERENCES [dbo].[Tag] ([tag_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Project side: sp_Project_Delete reassigns provenance and
    -- explicitly clears join rows; the sentinel Project(0) is never deleted.
    -- A second cascade path would also collide with the Tag->Project provenance
    -- FK, which is itself NO ACTION for the same reason.
    CONSTRAINT [FK_Project_Tag_Project] FOREIGN KEY ([project_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
