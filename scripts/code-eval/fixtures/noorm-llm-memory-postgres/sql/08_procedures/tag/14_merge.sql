-- Procedure: sp_Tag_Merge
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Merge"(
    p_source_tag_id INT,
    p_target_tag_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_source_tag_id = p_target_tag_id THEN
        RAISE EXCEPTION 'cannot merge tag into itself' USING ERRCODE = '22023';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "Tag" WHERE tag_id = p_source_tag_id) THEN
        RAISE EXCEPTION 'source Tag % not found', p_source_tag_id USING ERRCODE = '02000';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "Tag" WHERE tag_id = p_target_tag_id) THEN
        RAISE EXCEPTION 'target Tag % not found', p_target_tag_id USING ERRCODE = '02000';
    END IF;

    -- Project_Tag
    INSERT INTO "Project_Tag" (tag_id, project_id)
        SELECT p_target_tag_id, project_id FROM "Project_Tag" WHERE tag_id = p_source_tag_id
        ON CONFLICT DO NOTHING;
    DELETE FROM "Project_Tag" WHERE tag_id = p_source_tag_id;

    -- Memory_Tag
    INSERT INTO "Memory_Tag" (tag_id, memory_id)
        SELECT p_target_tag_id, memory_id FROM "Memory_Tag" WHERE tag_id = p_source_tag_id
        ON CONFLICT DO NOTHING;
    DELETE FROM "Memory_Tag" WHERE tag_id = p_source_tag_id;

    -- Artifact_Tag
    INSERT INTO "Artifact_Tag" (tag_id, artifact_id)
        SELECT p_target_tag_id, artifact_id FROM "Artifact_Tag" WHERE tag_id = p_source_tag_id
        ON CONFLICT DO NOTHING;
    DELETE FROM "Artifact_Tag" WHERE tag_id = p_source_tag_id;

    -- Milestone_Tag
    INSERT INTO "Milestone_Tag" (tag_id, milestone_id)
        SELECT p_target_tag_id, milestone_id FROM "Milestone_Tag" WHERE tag_id = p_source_tag_id
        ON CONFLICT DO NOTHING;
    DELETE FROM "Milestone_Tag" WHERE tag_id = p_source_tag_id;

    -- Task_Tag
    INSERT INTO "Task_Tag" (tag_id, milestone_id, task_no)
        SELECT p_target_tag_id, milestone_id, task_no FROM "Task_Tag" WHERE tag_id = p_source_tag_id
        ON CONFLICT DO NOTHING;
    DELETE FROM "Task_Tag" WHERE tag_id = p_source_tag_id;

    CALL "sp_Tag_Delete"(p_source_tag_id);
END;
$$;
