-- Procedure: sp_Milestone_Close
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Close"(
    p_milestone_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_task_no INT;
BEGIN
    CALL "sp_Milestone_SetTracking"(p_milestone_id, 'done', p_agent_id, p_reason);
    CALL "sp_Milestone_SetRelevance"(p_milestone_id, 'superseded', p_agent_id, p_reason);

    FOR v_task_no IN
        SELECT task_no
            FROM "Task"
            WHERE milestone_id = p_milestone_id
              AND tracking_status NOT IN ('done', 'abandoned')
    LOOP
        CALL "sp_Task_SetTracking"(p_milestone_id, v_task_no, 'abandoned', p_agent_id, p_reason);
    END LOOP;
END;
$$;
