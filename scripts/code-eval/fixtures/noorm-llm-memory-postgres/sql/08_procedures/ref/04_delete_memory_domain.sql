-- Procedure: sp_Ref_Delete_MemoryDomain
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Delete_MemoryDomain"(
    p_domain TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM "Memory" WHERE domain = p_domain) THEN
        RAISE EXCEPTION 'MemoryDomain % is in use and cannot be deleted', p_domain
            USING ERRCODE = '23503';
    END IF;

    DELETE FROM "MemoryDomain" WHERE domain = p_domain;
END;
$$;
