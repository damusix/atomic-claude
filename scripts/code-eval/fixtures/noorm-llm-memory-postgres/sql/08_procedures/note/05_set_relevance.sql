-- Procedure: sp_Note_SetRelevance
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Note_SetRelevance"(
    p_note_id INT,
    p_new_relevance_status TEXT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_from_status TEXT;
    v_transition_id INT;
BEGIN
    SELECT relevance_status INTO v_from_status
        FROM "Note"
        WHERE note_id = p_note_id;

    IF v_from_status IS NULL THEN
        RAISE EXCEPTION 'Note % not found', p_note_id USING ERRCODE = '02000';
    END IF;

    IF NOT "fn_IsRelevanceTransitionAllowed"(v_from_status, p_new_relevance_status) THEN
        RAISE EXCEPTION 'transition % -> % not allowed for note-relevance', v_from_status, p_new_relevance_status USING ERRCODE = '23514';
    END IF;

    UPDATE "Note"
        SET relevance_status = p_new_relevance_status,
            updated_at = NOW()
        WHERE note_id = p_note_id;

    INSERT INTO "StateTransition" (state_transition_type, agent_id, from_status, to_status, reason, occurred_at)
        VALUES ('note-relevance', p_agent_id, v_from_status, p_new_relevance_status, p_reason, NOW())
        RETURNING transition_id INTO v_transition_id;

    INSERT INTO "Note_StateTransition" (transition_id, note_id)
        VALUES (v_transition_id, p_note_id);
END;
$$;
