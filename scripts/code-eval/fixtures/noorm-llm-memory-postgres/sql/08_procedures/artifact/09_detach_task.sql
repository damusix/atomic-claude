-- Procedure: sp_Artifact_Detach_Task
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Detach_Task"(
    p_artifact_id INT,
    p_milestone_id INT,
    p_task_no INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Task_Artifact"
        WHERE milestone_id = p_milestone_id
          AND task_no = p_task_no
          AND artifact_id = p_artifact_id;
END;
$$;
