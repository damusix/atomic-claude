-- Procedure: sp_Ref_Delete_MemoryCategory
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Delete_MemoryCategory"(
    p_category TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM "Memory" WHERE category = p_category) THEN
        RAISE EXCEPTION 'MemoryCategory % is in use and cannot be deleted', p_category
            USING ERRCODE = '23503';
    END IF;

    DELETE FROM "MemoryCategory" WHERE category = p_category;
END;
$$;
