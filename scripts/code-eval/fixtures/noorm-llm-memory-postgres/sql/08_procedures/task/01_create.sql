-- Procedure: sp_Task_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE FUNCTION "sp_Task_Create"(
    p_milestone_id INT,
    p_title TEXT,
    p_content TEXT,
    p_reason TEXT,
    p_agent_id INT
)
RETURNS TABLE(milestone_id INT, task_no INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_task_no INT;
BEGIN
    v_task_no := "fn_NextTaskNo"(p_milestone_id);

    INSERT INTO "Task" (milestone_id, task_no, tracking_status, agent_id, title, content, reason)
        VALUES (p_milestone_id, v_task_no, 'not-started', p_agent_id, p_title, p_content, p_reason);

    RETURN QUERY SELECT p_milestone_id, v_task_no;
END;
$$;
