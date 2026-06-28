-- Procedure: sp_Artifact_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Update"(
    p_artifact_id INT,
    p_title TEXT,
    p_description TEXT,
    p_filepath TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Artifact"
        SET title = p_title,
            description = p_description,
            filepath = p_filepath,
            reason = p_reason,
            updated_at = NOW()
        WHERE artifact_id = p_artifact_id;
END;
$$;
