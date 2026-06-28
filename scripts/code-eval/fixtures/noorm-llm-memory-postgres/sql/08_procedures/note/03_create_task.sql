-- Procedure: sp_Note_Create_Task
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Note_Create_Task"(
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_milestone_id INT,
    p_task_no INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Note_Create_Task"(
    p_content TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_milestone_id INT,
    p_task_no INT
)
RETURNS TABLE(note_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_note_id INT;
BEGIN
    INSERT INTO "Note" (
        note_type,
        relevance_status,
        provenance_id,
        agent_id,
        content,
        reason
    )
    VALUES (
        'task',
        'active',
        p_provenance_id,
        p_agent_id,
        p_content,
        p_reason
    )
    RETURNING "Note".note_id INTO v_note_id;

    INSERT INTO "Task_Note" (note_id, milestone_id, task_no)
        VALUES (v_note_id, p_milestone_id, p_task_no);

    RETURN QUERY SELECT v_note_id;
END;
$$;
