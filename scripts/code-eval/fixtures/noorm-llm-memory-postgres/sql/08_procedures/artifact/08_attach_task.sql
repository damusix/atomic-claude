-- Procedure: sp_Artifact_Attach_Task
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Artifact_Attach_Task"(
    p_artifact_id INT,
    p_milestone_id INT,
    p_task_no INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Task_Artifact" (milestone_id, task_no, artifact_id)
        VALUES (p_milestone_id, p_task_no, p_artifact_id)
        ON CONFLICT DO NOTHING;
END;
$$;
