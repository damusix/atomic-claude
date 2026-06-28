CREATE TABLE [dbo].[Project_Note] (
    [note_id]    INT NOT NULL,
    [project_id] INT NOT NULL,
    [created_at] DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Project_Note] PRIMARY KEY ([note_id]),
    -- CASCADE from basetype: deleting the Note removes the subtype row.
    CONSTRAINT [FK_Project_Note_Note] FOREIGN KEY ([note_id])
        REFERENCES [dbo].[Note] ([note_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: avoid multi-cascade-path conflict. Note already cascades
    -- from Project via Note.provenance_id (when reassigned to sentinel) and
    -- this row would form a second cascade path. sp_Cleanup / sp_Project_Delete
    -- handle Project deletion explicitly before any orphaned subtype rows remain.
    CONSTRAINT [FK_Project_Note_Project] FOREIGN KEY ([project_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces Note.note_type = 'project' without a
    -- trigger. Calls fn_NoteIsOfType (declared in sql/03_validators/).
    -- Exclusivity across the three *_Note subtypes is naturally enforced:
    -- the parent Note has a single note_type, so only one discriminator
    -- CHECK can pass for any given note_id.
    CONSTRAINT [CK_Project_Note_NoteType]
        CHECK ([dbo].[fn_NoteIsOfType]([note_id], 'project') = 1)
);
