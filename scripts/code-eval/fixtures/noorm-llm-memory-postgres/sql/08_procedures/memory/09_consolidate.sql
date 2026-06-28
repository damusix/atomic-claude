-- Procedure: sp_Memory_Consolidate
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Consolidate"(
    p_canonical_memory_id INT,
    p_duplicate_memory_id INT,
    p_agent_id INT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_canonical_memory_id = p_duplicate_memory_id THEN
        RAISE EXCEPTION 'self-reference not allowed' USING ERRCODE = '22023';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "Memory" WHERE memory_id = p_canonical_memory_id) THEN
        RAISE EXCEPTION 'Memory % not found', p_canonical_memory_id USING ERRCODE = '02000';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "Memory" WHERE memory_id = p_duplicate_memory_id) THEN
        RAISE EXCEPTION 'Memory % not found', p_duplicate_memory_id USING ERRCODE = '02000';
    END IF;

    INSERT INTO "Related_Memory" (memory_id, related_memory_id, relation_verb, reason)
        VALUES (p_canonical_memory_id, p_duplicate_memory_id, 'supersedes', p_reason)
        ON CONFLICT DO NOTHING;

    INSERT INTO "Memory_Tag" (tag_id, memory_id)
        SELECT tag_id, p_canonical_memory_id
            FROM "Memory_Tag"
            WHERE memory_id = p_duplicate_memory_id
        ON CONFLICT DO NOTHING;

    DELETE FROM "Memory_Tag" WHERE memory_id = p_duplicate_memory_id;

    INSERT INTO "Project_Memory" (project_id, memory_id)
        SELECT project_id, p_canonical_memory_id
            FROM "Project_Memory"
            WHERE memory_id = p_duplicate_memory_id
        ON CONFLICT DO NOTHING;

    DELETE FROM "Project_Memory" WHERE memory_id = p_duplicate_memory_id;

    CALL "sp_Memory_SetRelevance"(p_duplicate_memory_id, 'superseded', p_agent_id, p_reason);
END;
$$;
