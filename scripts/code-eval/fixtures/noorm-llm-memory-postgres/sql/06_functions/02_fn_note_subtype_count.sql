-- Function: fn_NoteSubtypeCount
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Count how many of the three subtype tables (Project_Note, Milestone_Note,
-- Task_Note) contain a row for this note_id. Valid data always returns 1.
-- 0 = orphaned note (no subtype row). >1 = exclusivity violation.

CREATE OR REPLACE FUNCTION "fn_NoteSubtypeCount"(p_note_id INT)
RETURNS INT
LANGUAGE sql STABLE
AS $$
    SELECT
        (CASE WHEN EXISTS (SELECT 1 FROM "Project_Note"   WHERE note_id = p_note_id) THEN 1 ELSE 0 END)
      + (CASE WHEN EXISTS (SELECT 1 FROM "Milestone_Note" WHERE note_id = p_note_id) THEN 1 ELSE 0 END)
      + (CASE WHEN EXISTS (SELECT 1 FROM "Task_Note"      WHERE note_id = p_note_id) THEN 1 ELSE 0 END);
$$;
