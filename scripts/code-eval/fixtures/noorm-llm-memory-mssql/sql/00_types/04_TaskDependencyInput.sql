CREATE TYPE [dbo].[TaskDependencyInput] AS TABLE (
    [milestone_id]     INT NOT NULL,
    [task_no]          INT NOT NULL,
    [dep_milestone_id] INT NOT NULL,
    [dep_task_no]      INT NOT NULL,
    [dependency_verb]  VARCHAR(32) NOT NULL,
    [reason]           NVARCHAR(255) NOT NULL
);
