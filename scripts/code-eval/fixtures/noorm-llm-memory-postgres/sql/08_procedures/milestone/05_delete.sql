-- Procedure: sp_Milestone_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_Delete"(
    p_milestone_id INT,
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
    -- 1. Soft-delete the milestone itself.
    CALL "sp_Milestone_SetRelevance"(p_milestone_id, 'deleted', p_agent_id, p_reason);

    -- 2. Soft-delete all Notes attached directly to this milestone via Milestone_Note.
    FOR v_note_id IN
        SELECT mn.note_id
            FROM "Milestone_Note" mn
            JOIN "Note" n ON n.note_id = mn.note_id
            WHERE mn.milestone_id = p_milestone_id
              AND n.relevance_status <> 'deleted'
    LOOP
        SELECT relevance_status INTO v_from_status FROM "Note" WHERE note_id = v_note_id;

        IF v_from_status IS NULL THEN
            CONTINUE;
        END IF;

        IF NOT "fn_IsRelevanceTransitionAllowed"(v_from_status, 'deleted') THEN
            RAISE EXCEPTION 'transition % -> deleted not allowed for note-relevance (note %)', v_from_status, v_note_id USING ERRCODE = '23514';
        END IF;

        UPDATE "Note"
            SET relevance_status = 'deleted',
                updated_at = NOW()
            WHERE note_id = v_note_id;

        INSERT INTO "StateTransition" (
            state_transition_type, agent_id, from_status, to_status, reason, occurred_at
        ) VALUES (
            'note-relevance', p_agent_id, v_from_status, 'deleted', p_reason, NOW()
        )
        RETURNING transition_id INTO v_transition_id;

        INSERT INTO "Note_StateTransition" (transition_id, note_id)
            VALUES (v_transition_id, v_note_id);
    END LOOP;

    -- 3. Soft-delete all Notes attached to Tasks under this milestone via Task_Note.
    FOR v_note_id IN
        SELECT tn.note_id
            FROM "Task_Note" tn
            JOIN "Task" t ON t.milestone_id = tn.milestone_id AND t.task_no = tn.task_no
            JOIN "Note" n ON n.note_id = tn.note_id
            WHERE t.milestone_id = p_milestone_id
              AND n.relevance_status <> 'deleted'
    LOOP
        SELECT relevance_status INTO v_from_status FROM "Note" WHERE note_id = v_note_id;

        IF v_from_status IS NULL THEN
            CONTINUE;
        END IF;

        IF NOT "fn_IsRelevanceTransitionAllowed"(v_from_status, 'deleted') THEN
            RAISE EXCEPTION 'transition % -> deleted not allowed for note-relevance (note %)', v_from_status, v_note_id USING ERRCODE = '23514';
        END IF;

        UPDATE "Note"
            SET relevance_status = 'deleted',
                updated_at = NOW()
            WHERE note_id = v_note_id;

        INSERT INTO "StateTransition" (
            state_transition_type, agent_id, from_status, to_status, reason, occurred_at
        ) VALUES (
            'note-relevance', p_agent_id, v_from_status, 'deleted', p_reason, NOW()
        )
        RETURNING transition_id INTO v_transition_id;

        INSERT INTO "Note_StateTransition" (transition_id, note_id)
            VALUES (v_transition_id, v_note_id);
    END LOOP;
END;
$$;
