-- Procedure: sp_Memory_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Memory_Create"(
    p_content TEXT,
    p_domain TEXT,
    p_category TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_was_inferred BOOLEAN,
    p_was_observed BOOLEAN,
    p_was_evidenced BOOLEAN,
    p_was_user_provided BOOLEAN
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Memory_Create"(
    p_content TEXT,
    p_domain TEXT,
    p_category TEXT,
    p_reason TEXT,
    p_provenance_id INT,
    p_agent_id INT,
    p_was_inferred BOOLEAN,
    p_was_observed BOOLEAN,
    p_was_evidenced BOOLEAN,
    p_was_user_provided BOOLEAN
)
RETURNS TABLE(memory_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_memory_id INT;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM "MemoryDomain" WHERE domain = p_domain) THEN
        RAISE EXCEPTION 'unknown MemoryDomain: %', p_domain USING ERRCODE = '23503';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "MemoryCategory" WHERE category = p_category) THEN
        RAISE EXCEPTION 'unknown MemoryCategory: %', p_category USING ERRCODE = '23503';
    END IF;

    INSERT INTO "Memory" (
        content,
        domain,
        category,
        reason,
        provenance_id,
        agent_id,
        was_inferred,
        was_observed,
        was_evidenced,
        was_user_provided,
        relevance_status,
        last_accessed_at,
        access_count
    )
    VALUES (
        p_content,
        p_domain,
        p_category,
        p_reason,
        p_provenance_id,
        p_agent_id,
        COALESCE(p_was_inferred, false),
        COALESCE(p_was_observed, false),
        COALESCE(p_was_evidenced, false),
        COALESCE(p_was_user_provided, false),
        'active',
        NOW(),
        0
    )
    RETURNING "Memory".memory_id INTO v_memory_id;

    RETURN QUERY SELECT v_memory_id;
END;
$$;
