CREATE TABLE [dbo].[Note_StateTransition] (
    [transition_id] INT NOT NULL,
    [note_id]       INT NOT NULL,
    [created_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Note_StateTransition] PRIMARY KEY ([transition_id]),
    -- CASCADE from basetype: deleting the StateTransition removes the subtype row.
    CONSTRAINT [FK_Note_StateTransition_StateTransition] FOREIGN KEY ([transition_id])
        REFERENCES [dbo].[StateTransition] ([transition_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Note hard-delete is performed by sp_Cleanup after the
    -- expired-transition capture pass. The StateTransition basetype cascade
    -- then clears this subtype row. A direct Note cascade would conflict
    -- with the basetype cascade path.
    CONSTRAINT [FK_Note_StateTransition_Note] FOREIGN KEY ([note_id])
        REFERENCES [dbo].[Note] ([note_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces StateTransition.state_transition_type =
    -- 'note-relevance' without a trigger. Calls fn_StateTransitionIsOfType
    -- (declared in sql/03_validators/).
    CONSTRAINT [CK_Note_StateTransition_Type]
        CHECK ([dbo].[fn_StateTransitionIsOfType]([transition_id], 'note-relevance') = 1)
);
