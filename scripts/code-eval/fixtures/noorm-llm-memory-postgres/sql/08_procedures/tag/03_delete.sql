-- Procedure: sp_Tag_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Delete"(
    p_tag_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Tag" WHERE tag_id = p_tag_id;
END;
$$;
