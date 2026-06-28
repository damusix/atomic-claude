-- Procedure: sp_Memory_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Delete"(
    p_memory_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    CALL "sp_Memory_SetRelevance"(p_memory_id, 'deleted', p_agent_id, p_reason);
END;
$$;
