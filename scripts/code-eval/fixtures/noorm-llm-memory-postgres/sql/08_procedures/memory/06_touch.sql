-- Procedure: sp_Memory_Touch
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Touch"(
    p_memory_id INT,
    p_agent_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Memory"
        SET last_accessed_at = NOW(),
            access_count = access_count + 1
        WHERE memory_id = p_memory_id;
END;
$$;
