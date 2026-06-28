-- Procedure: sp_Artifact_Attach_Milestone
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Attach_Milestone"(
    p_artifact_id INT,
    p_milestone_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Milestone_Artifact" (milestone_id, artifact_id)
        VALUES (p_milestone_id, p_artifact_id)
        ON CONFLICT DO NOTHING;
END;
$$;
