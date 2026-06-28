CREATE TABLE [dbo].[Memory_StateTransition] (
    [transition_id] INT NOT NULL,
    [memory_id]     INT NOT NULL,
    [created_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Memory_StateTransition] PRIMARY KEY ([transition_id]),
    -- CASCADE from basetype: deleting the StateTransition removes the subtype row.
    CONSTRAINT [FK_Memory_StateTransition_StateTransition] FOREIGN KEY ([transition_id])
        REFERENCES [dbo].[StateTransition] ([transition_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Memory hard-delete is performed by sp_Cleanup after the
    -- expired-transition capture pass. The StateTransition basetype cascade
    -- then clears this subtype row. A direct Memory cascade would conflict
    -- with the basetype cascade path.
    CONSTRAINT [FK_Memory_StateTransition_Memory] FOREIGN KEY ([memory_id])
        REFERENCES [dbo].[Memory] ([memory_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: enforces StateTransition.state_transition_type =
    -- 'memory-relevance' without a trigger. Calls fn_StateTransitionIsOfType
    -- (declared in sql/03_validators/).
    CONSTRAINT [CK_Memory_StateTransition_Type]
        CHECK ([dbo].[fn_StateTransitionIsOfType]([transition_id], 'memory-relevance') = 1)
);
