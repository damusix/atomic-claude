-- Procedure: sp_Artifact_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Artifact_Create"(
    p_title TEXT,
    p_description TEXT,
    p_filepath TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Artifact_Create"(
    p_title TEXT,
    p_description TEXT,
    p_filepath TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
)
RETURNS TABLE(artifact_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_artifact_id INT;
BEGIN
    INSERT INTO "Artifact" (
        relevance_status,
        provenance_id,
        agent_id,
        title,
        description,
        filepath,
        reason
    )
    VALUES (
        'active',
        p_provenance_id,
        p_agent_id,
        p_title,
        p_description,
        p_filepath,
        p_reason
    )
    RETURNING "Artifact".artifact_id INTO v_artifact_id;

    RETURN QUERY SELECT v_artifact_id;
END;
$$;
