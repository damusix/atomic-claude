-- Procedure: sp_Tag_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Tag_Create"(
    p_name TEXT,
    p_description TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Tag_Create"(
    p_name TEXT,
    p_description TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT
)
RETURNS TABLE(tag_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_tag_id INT;
BEGIN
    IF EXISTS (SELECT 1 FROM "Tag" WHERE name = p_name) THEN
        RAISE EXCEPTION 'Tag name already exists: %', p_name USING ERRCODE = '23505';
    END IF;

    INSERT INTO "Tag" (name, description, reason, provenance_id, agent_id)
        VALUES (p_name, p_description, p_reason, p_provenance_id, p_agent_id)
        RETURNING "Tag".tag_id INTO v_tag_id;

    RETURN QUERY SELECT v_tag_id;
END;
$$;
