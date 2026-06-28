CREATE TABLE [dbo].[Task_Tag] (
    [tag_id]       INT NOT NULL,
    [milestone_id] INT NOT NULL,
    [task_no]      INT NOT NULL,
    [created_at]   DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
    CONSTRAINT [PK_Task_Tag] PRIMARY KEY ([tag_id], [milestone_id], [task_no]),
    -- CASCADE on Tag side: sp_Tag_Delete relies on this to wipe attachments.
    CONSTRAINT [FK_Task_Tag_Tag] FOREIGN KEY ([tag_id])
        REFERENCES [dbo].[Tag] ([tag_id])
        ON DELETE CASCADE ON UPDATE NO ACTION,
    -- NO ACTION on Task side: Task hard-delete already cascades from Milestone;
    -- a second cascade path through this composite FK would create a multi-
    -- cascade-path conflict (Tag->Task and Milestone->Task->Task_Tag). Cleanup
    -- removes orphan rows explicitly when Tasks are hard-deleted via Milestone.
    CONSTRAINT [FK_Task_Tag_Task] FOREIGN KEY ([milestone_id], [task_no])
        REFERENCES [dbo].[Task] ([milestone_id], [task_no])
        ON DELETE NO ACTION ON UPDATE NO ACTION
);
