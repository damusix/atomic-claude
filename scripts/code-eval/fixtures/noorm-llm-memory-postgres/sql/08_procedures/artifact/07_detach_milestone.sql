-- Procedure: sp_Artifact_Detach_Milestone
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Detach_Milestone"(
    p_artifact_id INT,
    p_milestone_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Milestone_Artifact"
        WHERE milestone_id = p_milestone_id
          AND artifact_id = p_artifact_id;
END;
$$;
