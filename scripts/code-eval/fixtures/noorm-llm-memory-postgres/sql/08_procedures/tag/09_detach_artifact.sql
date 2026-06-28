-- Procedure: sp_Tag_Detach_Artifact
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Detach_Artifact"(
    p_tag_id INT,
    p_artifact_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Artifact_Tag"
        WHERE tag_id = p_tag_id
          AND artifact_id = p_artifact_id;
END;
$$;
