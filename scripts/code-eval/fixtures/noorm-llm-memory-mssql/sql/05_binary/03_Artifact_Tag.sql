CREATE TABLE [dbo].[Artifact_Tag] (
    [tag_id]      INT NOT NULL,
    [artifact_id] INT NOT NULL,
    [created_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Artifact_Tag] PRIMARY KEY ([tag_id], [artifact_id]),
    -- CASCADE on Tag side: sp_Tag_Delete relies on this to wipe attachments.
    CONSTRAINT [FK_Artifact_Tag_Tag] FOREIGN KEY ([tag_id])
        REFERENCES [dbo].[Tag] ([tag_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Artifact side: Artifact delete is soft first; hard delete
    -- via sp_Cleanup handles the join row removal alongside the entity.
    CONSTRAINT [FK_Artifact_Tag_Artifact] FOREIGN KEY ([artifact_id])
        REFERENCES [dbo].[Artifact] ([artifact_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
