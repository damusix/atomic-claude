-- Procedure: sp_Milestone_SetTracking
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Milestone_SetTracking"(
    p_milestone_id INT,
    p_new_tracking_status TEXT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_from_status TEXT;
    v_transition_id INT;
BEGIN
    SELECT tracking_status INTO v_from_status
        FROM "Milestone"
        WHERE milestone_id = p_milestone_id;

    IF v_from_status IS NULL THEN
        RAISE EXCEPTION 'Milestone % not found', p_milestone_id USING ERRCODE = '02000';
    END IF;

    IF NOT "fn_IsTrackingTransitionAllowed"(v_from_status, p_new_tracking_status) THEN
        RAISE EXCEPTION 'transition % -> % not allowed for milestone-tracking', v_from_status, p_new_tracking_status USING ERRCODE = '23514';
    END IF;

    UPDATE "Milestone"
        SET tracking_status = p_new_tracking_status,
            updated_at = NOW()
        WHERE milestone_id = p_milestone_id;

    INSERT INTO "StateTransition" (
        state_transition_type, agent_id, from_status, to_status, reason, occurred_at
    ) VALUES (
        'milestone-tracking', p_agent_id, v_from_status, p_new_tracking_status, p_reason, NOW()
    )
    RETURNING transition_id INTO v_transition_id;

    INSERT INTO "Milestone_StateTransition" (transition_id, milestone_id)
        VALUES (v_transition_id, p_milestone_id);
END;
$$;
