CREATE TABLE [dbo].[StateTransition] (
    [transition_id]         INT IDENTITY(1,1) NOT NULL,
    [state_transition_type] VARCHAR(32) NOT NULL,
    [agent_id]              INT NOT NULL DEFAULT 0,
    [from_status]           VARCHAR(32) NOT NULL,
    [to_status]             VARCHAR(32) NOT NULL,
    [reason]                NVARCHAR(255) NOT NULL DEFAULT N'',
    [occurred_at]           DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [created_at]            DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    -- StateTransition is immutable — no updated_at by design.
    CONSTRAINT [PK_StateTransition] PRIMARY KEY ([transition_id]),
    -- NO ACTION on all FKs: reference table and sentinel-protected Agent use
    -- sentinel reassignment rather than cascades. from_status/to_status are
    -- intentionally unconstrained scalars (validated at proc layer against
    -- TrackingStatus or RelevanceStatus depending on state_transition_type).
    CONSTRAINT [FK_StateTransition_StateTransitionType] FOREIGN KEY ([state_transition_type])
        REFERENCES [dbo].[StateTransitionType] ([state_transition_type])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_StateTransition_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
