-- Procedure: sp_Agent_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Agent_Delete"(
    p_agent_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_agent_id = 0 THEN
        RAISE EXCEPTION 'Sentinel agent_id=0 is undeletable' USING ERRCODE = '23514';
    END IF;

    UPDATE "Project" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Note" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Tag" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Memory" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Artifact" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Milestone" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "Task" SET agent_id = 0 WHERE agent_id = p_agent_id;
    UPDATE "StateTransition" SET agent_id = 0 WHERE agent_id = p_agent_id;

    DELETE FROM "Agent" WHERE agent_id = p_agent_id;
END;
$$;
