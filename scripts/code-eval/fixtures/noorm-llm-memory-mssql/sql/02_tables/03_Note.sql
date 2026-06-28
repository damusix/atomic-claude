CREATE TABLE [dbo].[Note] (
    [note_id]          INT IDENTITY(1,1) NOT NULL,
    [note_type]        VARCHAR(32) NOT NULL,
    [relevance_status] VARCHAR(32) NOT NULL,
    [provenance_id]    INT NOT NULL DEFAULT 0,
    [agent_id]         INT NOT NULL DEFAULT 0,
    [content]          NVARCHAR(MAX) NOT NULL DEFAULT N'',
    [reason]           NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]       DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Note] PRIMARY KEY ([note_id]),
    -- NO ACTION on all FKs: reference tables and sentinel-protected entities (Agent, Project)
    -- use sentinel reassignment via cleanup procs rather than cascades.
    CONSTRAINT [FK_Note_NoteType] FOREIGN KEY ([note_type])
        REFERENCES [dbo].[NoteType] ([note_type])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Note_RelevanceStatus] FOREIGN KEY ([relevance_status])
        REFERENCES [dbo].[RelevanceStatus] ([relevance_status])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Note_Project] FOREIGN KEY ([provenance_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Note_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
