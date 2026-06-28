-- Procedure: sp_Milestone_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Milestone_Create"(
    p_title TEXT,
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Milestone_Create"(
    p_title TEXT,
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
)
RETURNS TABLE(milestone_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_milestone_id INT;
BEGIN
    INSERT INTO "Milestone" (
        tracking_status,
        relevance_status,
        provenance_id,
        agent_id,
        title,
        content,
        reason
    ) VALUES (
        'not-started',
        'active',
        p_provenance_id,
        p_agent_id,
        p_title,
        p_content,
        p_reason
    )
    RETURNING "Milestone".milestone_id INTO v_milestone_id;

    RETURN QUERY SELECT v_milestone_id;
END;
$$;
