-- Procedure: sp_Artifact_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Delete"(
    p_artifact_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    CALL "sp_Artifact_SetRelevance"(p_artifact_id, 'deleted', p_agent_id, p_reason);
END;
$$;
