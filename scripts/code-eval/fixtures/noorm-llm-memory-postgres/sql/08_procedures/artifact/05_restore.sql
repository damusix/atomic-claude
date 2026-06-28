-- Procedure: sp_Artifact_Restore
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Restore"(
    p_artifact_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    CALL "sp_Artifact_SetRelevance"(p_artifact_id, 'active', p_agent_id, p_reason);
END;
$$;
