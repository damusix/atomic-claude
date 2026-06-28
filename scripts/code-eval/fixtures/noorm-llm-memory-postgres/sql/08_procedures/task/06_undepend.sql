-- Procedure: sp_Task_Undepend
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Task_Undepend"(
    p_milestone_id INT,
    p_task_no INT,
    p_dep_milestone_id INT,
    p_dep_task_no INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Task_Dependency"
        WHERE milestone_id = p_milestone_id
          AND task_no = p_task_no
          AND dep_milestone_id = p_dep_milestone_id
          AND dep_task_no = p_dep_task_no;
END;
$$;
