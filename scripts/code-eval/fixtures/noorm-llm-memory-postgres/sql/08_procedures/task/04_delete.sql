-- Procedure: sp_Task_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)
--
-- Soft-deletes all Notes attached via Task_Note (inline-mirroring sp_Note_SetRelevance
-- with new_relevance_status = 'deleted'), then calls sp_Task_SetTracking to set
-- tracking_status = 'abandoned'. The note loop is inlined to avoid a forward dependency
-- on sp_Note_Delete during procedure load order.

CREATE OR REPLACE PROCEDURE "sp_Task_Delete"(
    p_milestone_id INT,
    p_task_no INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_note_id INT;
    v_from_status TEXT;
    v_transition_id INT;
BEGIN
    FOR v_note_id IN
        SELECT tn.note_id
            FROM "Task_Note" tn
            JOIN "Note" n ON n.note_id = tn.note_id
            WHERE tn.milestone_id = p_milestone_id
              AND tn.task_no = p_task_no
              AND n.relevance_status <> 'deleted'
    LOOP
        SELECT relevance_status INTO v_from_status
            FROM "Note"
            WHERE note_id = v_note_id;

        IF v_from_status IS NULL THEN
            CONTINUE;
        END IF;

        IF NOT "fn_IsRelevanceTransitionAllowed"(v_from_status, 'deleted') THEN
            RAISE EXCEPTION 'transition % -> deleted not allowed for note-relevance (note_id=%)', v_from_status, v_note_id USING ERRCODE = '23514';
        END IF;

        UPDATE "Note"
            SET relevance_status = 'deleted',
                updated_at = NOW()
            WHERE note_id = v_note_id;

        INSERT INTO "StateTransition" (state_transition_type, agent_id, from_status, to_status, reason, occurred_at)
            VALUES ('note-relevance', p_agent_id, v_from_status, 'deleted', p_reason, NOW())
            RETURNING transition_id INTO v_transition_id;

        INSERT INTO "Note_StateTransition" (transition_id, note_id)
            VALUES (v_transition_id, v_note_id);
    END LOOP;

    CALL "sp_Task_SetTracking"(p_milestone_id, p_task_no, 'abandoned', p_agent_id, p_reason);
END;
$$;
