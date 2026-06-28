-- Procedure: sp_Task_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Task_Update"(
    p_milestone_id INT,
    p_task_no INT,
    p_title TEXT,
    p_content TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE "Task"
        SET title = p_title,
            content = p_content,
            reason = p_reason,
            updated_at = NOW()
        WHERE milestone_id = p_milestone_id AND task_no = p_task_no;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Task (%, %) not found', p_milestone_id, p_task_no USING ERRCODE = '02000';
    END IF;
END;
$$;
