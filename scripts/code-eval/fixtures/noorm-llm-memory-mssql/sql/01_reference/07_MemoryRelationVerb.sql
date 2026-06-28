CREATE TABLE [dbo].[MemoryRelationVerb] (
    [verb_forward] VARCHAR(32) NOT NULL,
    [verb_backward] VARCHAR(32) NOT NULL,
    CONSTRAINT [PK_MemoryRelationVerb] PRIMARY KEY ([verb_forward])
);
