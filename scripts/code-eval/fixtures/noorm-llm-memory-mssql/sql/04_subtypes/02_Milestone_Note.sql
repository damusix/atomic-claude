CREATE TABLE [dbo].[Milestone_Note] (
    [note_id]      INT NOT NULL,
    [milestone_id] INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Milestone_Note] PRIMARY KEY ([note_id]),
    -- CASCADE from basetype: deleting the Note removes the subtype row.
    CONSTRAINT [FK_Milestone_Note_Note] FOREIGN KEY ([note_id])
        REFERENCES [dbo].[Note] ([note_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Milestone deletion is handled by sp_Milestone_Delete /
    -- sp_Cleanup, which soft-deletes attached Notes first; the basetype
    -- cascade on Note then removes this subtype row. A second cascade path
    -- here would be redundant and may conflict on multi-path graphs.
    CONSTRAINT [FK_Milestone_Note_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces Note.note_type = 'milestone' without a
    -- trigger. Calls fn_NoteIsOfType (declared in sql/03_validators/).
    -- Exclusivity across *_Note subtypes is enforced by the single
    -- note_type column on the parent Note row.
    CONSTRAINT [CK_Milestone_Note_NoteType]
        CHECK ([dbo].[fn_NoteIsOfType]([note_id], 'milestone') = 1)
);
