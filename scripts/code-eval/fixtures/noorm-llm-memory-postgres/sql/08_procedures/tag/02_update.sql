-- Procedure: sp_Tag_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Update"(
    p_tag_id INT,
    p_name TEXT,
    p_description TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Tag"
        SET name = p_name,
            description = p_description,
            reason = p_reason,
            updated_at = NOW()
        WHERE tag_id = p_tag_id;
END;
$$;
