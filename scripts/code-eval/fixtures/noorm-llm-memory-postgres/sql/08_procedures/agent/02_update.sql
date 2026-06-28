-- Procedure: sp_Agent_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Agent_Update"(
    p_agent_id INT,
    p_name TEXT,
    p_description TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_agent_id = 0 THEN
        RAISE EXCEPTION 'Sentinel agent_id=0 is immutable' USING ERRCODE = '23514';
    END IF;

    UPDATE "Agent"
        SET name = p_name,
            description = p_description,
            updated_at = NOW()
        WHERE agent_id = p_agent_id;
END;
$$;
