-- Procedure: sp_Tag_Attach_Task
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Attach_Task"(
    p_tag_id INT,
    p_milestone_id INT,
    p_task_no INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Task_Tag" (tag_id, milestone_id, task_no)
        VALUES (p_tag_id, p_milestone_id, p_task_no)
        ON CONFLICT DO NOTHING;
END;
$$;
