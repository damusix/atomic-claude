CREATE TABLE [dbo].[Memory] (
    [memory_id]         INT IDENTITY(1,1) NOT NULL,
    [domain]            VARCHAR(32) NOT NULL,
    [category]          VARCHAR(32) NOT NULL,
    [relevance_status]  VARCHAR(32) NOT NULL,
    [provenance_id]     INT NOT NULL DEFAULT 0,
    [agent_id]          INT NOT NULL DEFAULT 0,
    [content]           NVARCHAR(MAX) NOT NULL DEFAULT N'',
    [reason]            NVARCHAR(255) NOT NULL DEFAULT N'',
    [was_inferred]      BIT NOT NULL DEFAULT 0,
    [was_observed]      BIT NOT NULL DEFAULT 0,
    [was_evidenced]     BIT NOT NULL DEFAULT 0,
    [was_user_provided] BIT NOT NULL DEFAULT 0,
    [last_accessed_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [access_count]      INT NOT NULL DEFAULT 0,
    [created_at]        DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]        DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Memory] PRIMARY KEY ([memory_id]),
    -- NO ACTION on all FKs: reference tables and sentinel-protected entities use
    -- sentinel reassignment via cleanup procs rather than cascades.
    CONSTRAINT [FK_Memory_MemoryDomain] FOREIGN KEY ([domain])
        REFERENCES [dbo].[MemoryDomain] ([domain])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Memory_MemoryCategory] FOREIGN KEY ([category])
        REFERENCES [dbo].[MemoryCategory] ([category])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Memory_RelevanceStatus] FOREIGN KEY ([relevance_status])
        REFERENCES [dbo].[RelevanceStatus] ([relevance_status])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Memory_Project] FOREIGN KEY ([provenance_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    CONSTRAINT [FK_Memory_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
