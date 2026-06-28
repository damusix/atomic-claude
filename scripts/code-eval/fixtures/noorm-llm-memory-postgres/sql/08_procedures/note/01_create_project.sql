-- Procedure: sp_Note_Create_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Note_Create_Project"(
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_project_id INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Note_Create_Project"(
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_project_id INT
)
RETURNS TABLE(note_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_note_id INT;
BEGIN
    INSERT INTO "Note" (
        note_type,
        relevance_status,
        provenance_id,
        agent_id,
        content,
        reason
    )
    VALUES (
        'project',
        'active',
        p_provenance_id,
        p_agent_id,
        p_content,
        p_reason
    )
    RETURNING "Note".note_id INTO v_note_id;

    INSERT INTO "Project_Note" (note_id, project_id)
        VALUES (v_note_id, p_project_id);

    RETURN QUERY SELECT v_note_id;
END;
$$;
