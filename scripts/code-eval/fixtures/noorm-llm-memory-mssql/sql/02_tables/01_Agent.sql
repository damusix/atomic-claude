CREATE TABLE [dbo].[Agent] (
    [agent_id]    INT IDENTITY(1,1) NOT NULL,
    [name]        NVARCHAR(255) NOT NULL DEFAULT N'',
    [description] NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]  DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Agent] PRIMARY KEY ([agent_id])
);
