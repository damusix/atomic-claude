-- Procedure: sp_Memory_Relate
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Relate"(
    p_memory_id INT,
    p_related_memory_id INT,
    p_relation_verb TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_memory_id = p_related_memory_id THEN
        RAISE EXCEPTION 'self-reference not allowed' USING ERRCODE = '22023';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "MemoryRelationVerb" WHERE verb_forward = p_relation_verb) THEN
        RAISE EXCEPTION 'unknown MemoryRelationVerb: %', p_relation_verb USING ERRCODE = '23503';
    END IF;

    INSERT INTO "Related_Memory" (memory_id, related_memory_id, relation_verb, reason)
        VALUES (p_memory_id, p_related_memory_id, p_relation_verb, p_reason)
        ON CONFLICT DO NOTHING;
END;
$$;
