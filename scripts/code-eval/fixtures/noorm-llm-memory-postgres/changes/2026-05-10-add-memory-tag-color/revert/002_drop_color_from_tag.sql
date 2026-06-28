-- Drop the `color` column from Tag. Safe because vw_Tag was rebuilt without it
-- in 001_recreate_vw_tag_without_color.sql.

ALTER TABLE "Tag" DROP COLUMN IF EXISTS color;
