-- Procedure: sp_Tag_Attach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Attach_Project"(
    p_tag_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Project_Tag" (tag_id, project_id)
        VALUES (p_tag_id, p_project_id)
        ON CONFLICT DO NOTHING;
END;
$$;
