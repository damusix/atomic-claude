# Changelog


## Description

Add a `color` column to `Tag` so the LLM-facing UI can group tags visually.


## Changes

- `change/001_add-color-column.sql` — `ALTER TABLE [Tag] ADD [color] NVARCHAR(16) NOT NULL DEFAULT N''`. Empty string default keeps the no-NULL invariant for existing rows.
- `change/002_refresh_vw_Tag.sql` — `CREATE OR ALTER VIEW [vw_Tag]` to surface `color` in every UNION branch.


## Impact

- Existing Tag rows pick up `color = N''`. No reads break (the column is appended).
- Consumers that select all columns from `vw_Tag` will see a new column; positional consumers (rare) will need to update.
- Reverts cleanly: revert drops the column (after dropping the default constraint) and rolls `vw_Tag` back to its pre-color shape.
