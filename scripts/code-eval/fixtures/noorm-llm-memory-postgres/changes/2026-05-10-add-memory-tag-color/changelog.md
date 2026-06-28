# Changelog


## Description

Adds a `color VARCHAR(16) NOT NULL DEFAULT ''` column to `Tag` and threads it through `vw_Tag` so the read API surfaces it. Demonstrates the noorm change system end-to-end with both forward and revert SQL.


## Changes

- `change/001_add_color_to_tag.sql` — adds `Tag.color` (idempotent via `IF NOT EXISTS`).
- `change/002_recreate_vw_tag.sql` — DROP+CREATE `vw_Tag` with `color` in all 5 UNION ALL branches.
- `revert/001_recreate_vw_tag_without_color.sql` — restores `vw_Tag` to the pre-change shape (must run before the column drop because the view depends on it).
- `revert/002_drop_color_from_tag.sql` — removes the `color` column.


## Impact

- Forward: idempotent (`IF NOT EXISTS` on the column add). Default value `''` preserves the project's no-NULL invariant for any pre-existing rows.
- Revert: rebuilds the view first, then drops the column. Order is enforced by the numeric prefix on the revert files.
