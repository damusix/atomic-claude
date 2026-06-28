-- Procedure: sp_Ref_Create_MemoryCategory
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Create_MemoryCategory"(
    p_category TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "MemoryCategory" (category)
        VALUES (p_category)
        ON CONFLICT DO NOTHING;
END;
$$;
