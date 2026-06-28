-- Procedure: sp_Memory_Unrelate
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Memory_Unrelate"(
    p_memory_id INT,
    p_related_memory_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Related_Memory"
        WHERE memory_id = p_memory_id
          AND related_memory_id = p_related_memory_id;
END;
$$;
