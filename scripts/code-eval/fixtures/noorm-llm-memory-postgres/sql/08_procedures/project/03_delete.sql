-- Procedure: sp_Project_Delete
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Project_Delete"(
    p_project_id INT
)
LANGUAGE plpgsql
AS $$
DECLARE
    v_note_id INT;
    v_from_status TEXT;
    v_transition_id INT;
BEGIN
    IF p_project_id = 0 THEN
        RAISE EXCEPTION 'Sentinel project_id=0 is undeletable' USING ERRCODE = '23514';
    END IF;

    -- Reassign provenance on every entity carrying provenance_id = this project
    -- to the sentinel project (0). Tables: Note, Tag, Memory, Artifact, Milestone.
    UPDATE "Note"      SET provenance_id = 0 WHERE provenance_id = p_project_id;
    UPDATE "Tag"       SET provenance_id = 0 WHERE provenance_id = p_project_id;
    UPDATE "Memory"    SET provenance_id = 0 WHERE provenance_id = p_project_id;
    UPDATE "Artifact"  SET provenance_id = 0 WHERE provenance_id = p_project_id;
    UPDATE "Milestone" SET provenance_id = 0 WHERE provenance_id = p_project_id;

    -- Soft-delete every Note attached to this project via Project_Note.
    -- Mirrors sp_Note_SetRelevance(note_id, 'deleted', agent_id=0, reason='Project deleted: cascade')
    -- inline to avoid cross-file proc dependencies.
    FOR v_note_id IN
        SELECT pn.note_id
            FROM "Project_Note" pn
            INNER JOIN "Note" n ON n.note_id = pn.note_id
            WHERE pn.project_id = p_project_id
              AND n.relevance_status <> 'deleted'
    LOOP
        SELECT relevance_status INTO v_from_status
            FROM "Note"
            WHERE note_id = v_note_id;

        IF NOT "fn_IsRelevanceTransitionAllowed"(v_from_status, 'deleted') THEN
            RAISE EXCEPTION 'transition % -> deleted not allowed for note-relevance (note_id=%)', v_from_status, v_note_id USING ERRCODE = '23514';
        END IF;

        UPDATE "Note"
            SET relevance_status = 'deleted',
                updated_at = NOW()
            WHERE note_id = v_note_id;

        INSERT INTO "StateTransition" (state_transition_type, agent_id, from_status, to_status, reason, occurred_at)
            VALUES ('note-relevance', 0, v_from_status, 'deleted', 'Project deleted: cascade', NOW())
            RETURNING transition_id INTO v_transition_id;

        INSERT INTO "Note_StateTransition" (transition_id, note_id)
            VALUES (v_transition_id, v_note_id);
    END LOOP;

    -- Hard delete the Project row. FK ON DELETE CASCADE removes
    -- Project_Note, Project_Tag, Project_Memory, Project_Milestone.
    DELETE FROM "Project" WHERE project_id = p_project_id;
END;
$$;
