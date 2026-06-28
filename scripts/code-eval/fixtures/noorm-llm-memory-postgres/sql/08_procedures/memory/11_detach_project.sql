-- Procedure: sp_Memory_Detach_Project
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Detach_Project"(
    p_memory_id INT,
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Project_Memory"
        WHERE project_id = p_project_id
          AND memory_id = p_memory_id;
END;
$$;
