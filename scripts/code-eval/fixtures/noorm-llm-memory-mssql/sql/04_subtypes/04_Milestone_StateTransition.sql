CREATE TABLE [dbo].[Milestone_StateTransition] (
    [transition_id] INT NOT NULL,
    [milestone_id]  INT NOT NULL,
    [created_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Milestone_StateTransition] PRIMARY KEY ([transition_id]),
    -- CASCADE from basetype: deleting the StateTransition removes the subtype row.
    CONSTRAINT [FK_Milestone_StateTransition_StateTransition] FOREIGN KEY ([transition_id])
        REFERENCES [dbo].[StateTransition] ([transition_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: Milestone hard-delete is orchestrated by sp_Cleanup, which
    -- captures expired transition_ids BEFORE the entity delete and removes
    -- the StateTransition basetype rows in a second pass. A direct cascade
    -- here would race with the basetype cascade and risk multi-path conflict.
    CONSTRAINT [FK_Milestone_StateTransition_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- Discriminator CHECK: Milestone is the only entity with TWO state-axis
    -- transition types ('milestone-tracking' AND 'milestone-relevance').
    -- The two-axis helper validator keeps this CHECK a single readable
    -- expression. Single-axis StateTransition subtypes use fn_StateTransitionIsOfType.
    CONSTRAINT [CK_Milestone_StateTransition_Type]
        CHECK ([dbo].[fn_StateTransitionIsMilestoneAxis]([transition_id]) = 1)
);
