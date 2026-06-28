CREATE TABLE [dbo].[Related_Memory] (
    [memory_id]         INT NOT NULL,
    [related_memory_id] INT NOT NULL,
    [relation_verb]     VARCHAR(32) NOT NULL,
    [reason]            NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]        DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Related_Memory] PRIMARY KEY ([memory_id], [related_memory_id]),
    -- CASCADE on the primary memory side: when the source Memory is hard-deleted
    -- by sp_Cleanup, its outgoing relations die with it. Memory owns this row.
    CONSTRAINT [FK_Related_Memory_Memory] FOREIGN KEY ([memory_id])
        REFERENCES [dbo].[Memory] ([memory_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on the related memory side: MSSQL forbids two cascade paths to
    -- the same table (Memory). The cleanup procedure must DELETE related rows
    -- pointing at the deleted memory before the entity row itself is removed.
    CONSTRAINT [FK_Related_Memory_Related] FOREIGN KEY ([related_memory_id])
        REFERENCES [dbo].[Memory] ([memory_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- NO ACTION on verb: sp_Ref_Delete_MemoryRelationVerb (or equivalent guard)
    -- protects the reference table from removal while in use.
    CONSTRAINT [FK_Related_Memory_Verb] FOREIGN KEY ([relation_verb])
        REFERENCES [dbo].[MemoryRelationVerb] ([verb_forward])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
