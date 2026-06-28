-- Procedure: sp_Tag_Detach_Milestone
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Detach_Milestone"(
    p_tag_id INT,
    p_milestone_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Milestone_Tag"
        WHERE tag_id = p_tag_id
          AND milestone_id = p_milestone_id;
END;
$$;
