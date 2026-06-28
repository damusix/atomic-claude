CREATE TABLE [dbo].[Task] (
    [milestone_id]    INT NOT NULL,
    [task_no]         INT NOT NULL,
    [tracking_status] VARCHAR(32) NOT NULL,
    [agent_id]        INT NOT NULL DEFAULT 0,
    [title]           NVARCHAR(255) NOT NULL DEFAULT N'',
    [content]         NVARCHAR(MAX) NOT NULL DEFAULT N'',
    [reason]          NVARCHAR(255) NOT NULL DEFAULT N'',
    [created_at]      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    [updated_at]      DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task] PRIMARY KEY ([milestone_id], [task_no]),
    -- CASCADE: tasks cannot exist without their parent Milestone (hierarchic child).
    -- sp_Cleanup hard-deletes expired Milestones; child Tasks die with them.
    CONSTRAINT [FK_Task_Milestone] FOREIGN KEY ([milestone_id])
        REFERENCES [dbo].[Milestone] ([milestone_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION: reference table, no cascade needed
    CONSTRAINT [FK_Task_TrackingStatus] FOREIGN KEY ([tracking_status])
        REFERENCES [dbo].[TrackingStatus] ([tracking_status])
        ON DELETE NO ACTION ON UPDATE NO ACTION,
    -- NO ACTION: sp_Agent_Delete reassigns Task.agent_id to sentinel 0
    CONSTRAINT [FK_Task_Agent] FOREIGN KEY ([agent_id])
        REFERENCES [dbo].[Agent] ([agent_id])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
