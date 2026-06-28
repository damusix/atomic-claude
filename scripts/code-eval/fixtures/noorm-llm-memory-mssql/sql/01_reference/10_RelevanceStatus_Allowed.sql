CREATE TABLE [dbo].[RelevanceStatus_Allowed] (
    [from_status] VARCHAR(32) NOT NULL,
    [to_status] VARCHAR(32) NOT NULL,
    CONSTRAINT [PK_RelevanceStatus_Allowed] PRIMARY KEY ([from_status], [to_status]),
    CONSTRAINT [FK_RelevanceStatus_Allowed_From] FOREIGN KEY ([from_status]) REFERENCES [dbo].[RelevanceStatus] ([relevance_status]) ON DELETE CASCADE,
    CONSTRAINT [FK_RelevanceStatus_Allowed_To] FOREIGN KEY ([to_status]) REFERENCES [dbo].[RelevanceStatus] ([relevance_status])
);
