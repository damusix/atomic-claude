CREATE TABLE [dbo].[Project_Memory] (
    [project_id] INT NOT NULL,
    [memory_id]  INT NOT NULL,
    [created_at] DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Project_Memory] PRIMARY KEY ([project_id], [memory_id]),
    -- NO ACTION on Project side: sp_Project_Delete explicitly clears join rows
    -- and reassigns provenance to sentinel 0; the sentinel itself is undeletable.
    CONSTRAINT [FK_Project_Memory_Project] FOREIGN KEY ([project_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- CASCADE on Memory side: Memory hard-delete via sp_Cleanup must remove
    -- this association row automatically. Memory owns the lifecycle here.
    CONSTRAINT [FK_Project_Memory_Memory] FOREIGN KEY ([memory_id])
        REFERENCES [dbo].[Memory] ([memory_id])
        ON DELETE CASCADE ON UPDATE NO ACTION
);
