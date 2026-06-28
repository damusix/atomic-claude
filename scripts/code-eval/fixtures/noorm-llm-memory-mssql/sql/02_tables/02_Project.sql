CREATE TABLE [dbo].[Project] (
    [project_id]  INT IDENTITY(1,1) NOT NULL,
    [agent_id]    INT NOT NULL DEFAULT 0,
    [name]        NVARCHAR(255) NOT NULL DEFAULT N'',
    [filepath]    NVARCHAR(255) NOT NULL DEFAULT N'',
    [git_repo]    NVARCHAR(255) NOT NULL DEFAULT N'',
    [main_branch] NVARCHAR(255) NOT NULL DEFAULT N'',
    [git_url]     NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Project] PRIMARY KEY ([project_id]),
    -- NO ACTION because: sp_Agent_Delete reassigns Project.agent_id to sentinel 0 instead of cascading
    CONSTRAINT [FK_Project_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
