-- Procedure: sp_Ref_Create_MemoryDomain
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Create_MemoryDomain"(
    p_domain TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "MemoryDomain" (domain)
        VALUES (p_domain)
        ON CONFLICT DO NOTHING;
END;
$$;
