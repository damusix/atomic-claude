-- Procedure: sp_Cleanup
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Cleanup"(
    p_ttl_days INT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_cutoff TIMESTAMPTZ := NOW() - (p_ttl_days || ' days')::INTERVAL;
BEGIN
    CREATE TEMP TABLE expired_transitions (transition_id INT) ON COMMIT DROP;

    INSERT INTO expired_transitions (transition_id)
        SELECT mst.transition_id
        FROM "Memory_StateTransition" mst
        INNER JOIN "Memory" m ON m.memory_id = mst.memory_id
        WHERE m.relevance_status = 'deleted' AND m.updated_at < v_cutoff;

    INSERT INTO expired_transitions (transition_id)
        SELECT nst.transition_id
        FROM "Note_StateTransition" nst
        INNER JOIN "Note" n ON n.note_id = nst.note_id
        WHERE n.relevance_status = 'deleted' AND n.updated_at < v_cutoff;

    INSERT INTO expired_transitions (transition_id)
        SELECT ast.transition_id
        FROM "Artifact_StateTransition" ast
        INNER JOIN "Artifact" a ON a.artifact_id = ast.artifact_id
        WHERE a.relevance_status = 'deleted' AND a.updated_at < v_cutoff;

    INSERT INTO expired_transitions (transition_id)
        SELECT mst.transition_id
        FROM "Milestone_StateTransition" mst
        INNER JOIN "Milestone" mi ON mi.milestone_id = mst.milestone_id
        WHERE mi.relevance_status = 'deleted' AND mi.updated_at < v_cutoff;

    INSERT INTO expired_transitions (transition_id)
        SELECT tst.transition_id
        FROM "Task_StateTransition" tst
        INNER JOIN "Milestone" mi ON mi.milestone_id = tst.milestone_id
        WHERE mi.relevance_status = 'deleted' AND mi.updated_at < v_cutoff;

    DELETE FROM "Memory"    WHERE relevance_status = 'deleted' AND updated_at < v_cutoff;
    DELETE FROM "Note"      WHERE relevance_status = 'deleted' AND updated_at < v_cutoff;
    DELETE FROM "Artifact"  WHERE relevance_status = 'deleted' AND updated_at < v_cutoff;
    DELETE FROM "Milestone" WHERE relevance_status = 'deleted' AND updated_at < v_cutoff;

    DELETE FROM "StateTransition"
        WHERE transition_id IN (SELECT transition_id FROM expired_transitions);
END;
$$;
