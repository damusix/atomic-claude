-- Procedure: sp_Agent_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Agent_Create"(
    p_name TEXT,
    p_description TEXT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Agent_Create"(
    p_name TEXT,
    p_description TEXT
)
RETURNS TABLE(agent_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_agent_id INT;
BEGIN
    INSERT INTO "Agent" (name, description)
        VALUES (p_name, p_description)
        RETURNING "Agent".agent_id INTO v_agent_id;

    RETURN QUERY SELECT v_agent_id;
END;
$$;
