-- Function: fn_NoteMatchesSubtype
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Given a note_id and its declared note_type, check that the subtype row
-- lives in the correct table:
--   'project'   -> row exists in Project_Note
--   'milestone' -> row exists in Milestone_Note
--   'task'      -> row exists in Task_Note
-- Returns true if the row is in the right table. False if missing or mismatched.

CREATE OR REPLACE FUNCTION "fn_NoteMatchesSubtype"(p_note_id INT, p_note_type VARCHAR(32))
RETURNS BOOLEAN
LANGUAGE sql STABLE
AS $$
    SELECT CASE p_note_type
        WHEN 'project'   THEN EXISTS (SELECT 1 FROM "Project_Note"   WHERE note_id = p_note_id)
        WHEN 'milestone' THEN EXISTS (SELECT 1 FROM "Milestone_Note" WHERE note_id = p_note_id)
        WHEN 'task'      THEN EXISTS (SELECT 1 FROM "Task_Note"      WHERE note_id = p_note_id)
        ELSE false
    END;
$$;
