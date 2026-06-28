-- Procedure: sp_Tag_Attach_Artifact
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Attach_Artifact"(
    p_tag_id INT,
    p_artifact_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Artifact_Tag" (tag_id, artifact_id)
        VALUES (p_tag_id, p_artifact_id)
        ON CONFLICT DO NOTHING;
END;
$$;
