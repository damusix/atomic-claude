-- Drop the [color] column and its default constraint.
-- DROP CONSTRAINT must come first because the DEFAULT depends on the column.

ALTER TABLE [dbo].[Tag] DROP CONSTRAINT [DF_Tag_color];
ALTER TABLE [dbo].[Tag] DROP COLUMN [color];
