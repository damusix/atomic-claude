-- Procedure: sp_Milestone_Attach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Attach_Project"(
    p_milestone_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Project_Milestone" (project_id, milestone_id)
        VALUES (p_project_id, p_milestone_id)
        ON CONFLICT DO NOTHING;
END;
$$;
