CREATE TABLE [dbo].[Memory_Tag] (
    [tag_id]     INT NOT NULL,
    [memory_id]  INT NOT NULL,
    [created_at] DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Memory_Tag] PRIMARY KEY ([tag_id], [memory_id]),
    -- CASCADE on Tag side: sp_Tag_Delete relies on this to wipe attachments.
    CONSTRAINT [FK_Memory_Tag_Tag] FOREIGN KEY ([tag_id])
        REFERENCES [dbo].[Tag] ([tag_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Memory side: Memory delete is soft (relevance_status='deleted')
    -- first; hard delete via sp_Cleanup will cascade through Memory's other paths
    -- and naturally collect this row when the row's other parent (Tag) is deleted
    -- or via explicit DELETE in cleanup.
    CONSTRAINT [FK_Memory_Tag_Memory] FOREIGN KEY ([memory_id])
        REFERENCES [dbo].[Memory] ([memory_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
