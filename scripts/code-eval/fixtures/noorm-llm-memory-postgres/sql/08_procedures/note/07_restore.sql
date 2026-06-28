-- Procedure: sp_Note_Restore
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Note_Restore"(
    p_note_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    CALL "sp_Note_SetRelevance"(p_note_id, 'active', p_agent_id, p_reason);
END;
$$;
