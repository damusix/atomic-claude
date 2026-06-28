-- Add a color column to Tag for visual grouping in the LLM-facing UI.
-- NVARCHAR(16) is enough for hex codes ('#a8b9c4'), CSS named colors,
-- or any short identifier. NOT NULL with empty default keeps the
-- "no NULLs anywhere" invariant.

ALTER TABLE [dbo].[Tag] ADD [color] NVARCHAR(16) NOT NULL CONSTRAINT [DF_Tag_color] DEFAULT N'';
