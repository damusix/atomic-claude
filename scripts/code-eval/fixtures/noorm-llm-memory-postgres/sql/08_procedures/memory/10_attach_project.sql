-- Procedure: sp_Memory_Attach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Attach_Project"(
    p_memory_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Project_Memory" (project_id, memory_id)
        VALUES (p_project_id, p_memory_id)
        ON CONFLICT DO NOTHING;
END;
$$;
