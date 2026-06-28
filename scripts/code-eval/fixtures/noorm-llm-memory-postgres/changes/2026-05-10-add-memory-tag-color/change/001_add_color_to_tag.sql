-- Add a `color` column to Tag for UI/visualization affordances.
-- Idempotent so re-running this change in dev environments is safe.

ALTER TABLE "Tag" ADD COLUMN IF NOT EXISTS color VARCHAR(16) NOT NULL DEFAULT '';
