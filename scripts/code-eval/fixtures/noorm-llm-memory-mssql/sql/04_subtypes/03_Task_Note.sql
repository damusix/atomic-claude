CREATE TABLE [dbo].[Task_Note] (
    [note_id]      INT NOT NULL,
    [milestone_id] INT NOT NULL,
    [task_no]      INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task_Note] PRIMARY KEY ([note_id]),
    -- CASCADE from basetype: deleting the Note removes the subtype row.
    CONSTRAINT [FK_Task_Note_Note] FOREIGN KEY ([note_id])
        REFERENCES [dbo].[Note] ([note_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Task deletion (or its parent Milestone's hard delete)
    -- already cascades to Note via sp_Note_Delete + the basetype cascade.
    -- A second cascade path from Task to this subtype row would create a
    -- multi-path conflict at the engine level and is unnecessary.
    CONSTRAINT [FK_Task_Note_Task] FOREIGN KEY ([milestone_id], [task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces Note.note_type = 'task' without a
    -- trigger. Calls fn_NoteIsOfType (declared in sql/03_validators/).
    -- Exclusivity across *_Note subtypes is enforced by the single
    -- note_type column on the parent Note row.
    CONSTRAINT [CK_Task_Note_NoteType]
        CHECK ([dbo].[fn_NoteIsOfType]([note_id], 'task') = 1)
);
