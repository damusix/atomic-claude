-- Procedure: sp_Memory_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Update"(
    p_memory_id INT,
    p_content TEXT,
    p_domain TEXT,
    p_category TEXT,
    p_reason TEXT,
    p_was_inferred BOOLEAN,
    p_was_observed BOOLEAN,
    p_was_evidenced BOOLEAN,
    p_was_user_provided BOOLEAN
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM "MemoryDomain" WHERE domain = p_domain) THEN
        RAISE EXCEPTION 'unknown MemoryDomain: %', p_domain USING ERRCODE = '23503';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "MemoryCategory" WHERE category = p_category) THEN
        RAISE EXCEPTION 'unknown MemoryCategory: %', p_category USING ERRCODE = '23503';
    END IF;

    UPDATE "Memory"
        SET content = p_content,
            domain = p_domain,
            category = p_category,
            reason = p_reason,
            was_inferred = COALESCE(p_was_inferred, was_inferred),
            was_observed = COALESCE(p_was_observed, was_observed),
            was_evidenced = COALESCE(p_was_evidenced, was_evidenced),
            was_user_provided = COALESCE(p_was_user_provided, was_user_provided),
            updated_at = NOW()
        WHERE memory_id = p_memory_id;
END;
$$;
