-- Procedure: sp_Milestone_Detach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Detach_Project"(
    p_milestone_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Project_Milestone"
        WHERE project_id = p_project_id
          AND milestone_id = p_milestone_id;
END;
$$;
