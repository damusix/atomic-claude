-- Procedure: sp_Milestone_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Update"(
    p_milestone_id INT,
    p_title TEXT,
    p_content TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Milestone"
        SET title = p_title,
            content = p_content,
            reason = p_reason,
            updated_at = NOW()
        WHERE milestone_id = p_milestone_id;
END;
$$;
