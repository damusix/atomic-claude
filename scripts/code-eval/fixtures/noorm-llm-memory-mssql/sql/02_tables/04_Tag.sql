CREATE TABLE [dbo].[Tag] (
    [tag_id]        INT IDENTITY(1,1) NOT NULL,
    [provenance_id] INT NOT NULL DEFAULT 0,
    [agent_id]      INT NOT NULL DEFAULT 0,
    [name]          NVARCHAR(255) NOT NULL DEFAULT N'',
    [description]   NVARCHAR(255) NOT NULL DEFAULT N'',
    [reason]        NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]    DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Tag] PRIMARY KEY ([tag_id]),
    CONSTRAINT [UQ_Tag_name] UNIQUE ([name]),
    -- NO ACTION: sp_Project_Delete reassigns Tag.provenance_id to sentinel 0 explicitly
    CONSTRAINT [FK_Tag_Project] FOREIGN KEY ([provenance_id])
        REFERENCES [dbo].[Project] ([project_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- NO ACTION: sp_Agent_Delete reassigns Tag.agent_id to sentinel 0 explicitly
    CONSTRAINT [FK_Tag_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
