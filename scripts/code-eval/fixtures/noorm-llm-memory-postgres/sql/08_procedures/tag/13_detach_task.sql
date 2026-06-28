-- Procedure: sp_Tag_Detach_Task
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Detach_Task"(
    p_tag_id INT,
    p_milestone_id INT,
    p_task_no INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Task_Tag"
        WHERE tag_id = p_tag_id
          AND milestone_id = p_milestone_id
          AND task_no = p_task_no;
END;
$$;
