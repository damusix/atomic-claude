-- Procedure: sp_Note_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Note_Update"(
    p_note_id INT,
    p_content TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Note"
        SET content = p_content,
            reason = p_reason,
            updated_at = NOW()
        WHERE note_id = p_note_id;
END;
$$;
