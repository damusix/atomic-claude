-- Procedure: sp_Milestone_Restore
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Restore"(
    p_milestone_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    CALL "sp_Milestone_SetRelevance"(p_milestone_id, 'active', p_agent_id, p_reason);
END;
$$;
