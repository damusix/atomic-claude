-- Procedure: sp_Tag_Detach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Detach_Project"(
    p_tag_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Project_Tag"
        WHERE tag_id = p_tag_id
          AND project_id = p_project_id;
END;
$$;
