-- Procedure: sp_Tag_Detach_Memory
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Detach_Memory"(
    p_tag_id INT,
    p_memory_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM "Memory_Tag"
        WHERE tag_id = p_tag_id
          AND memory_id = p_memory_id;
END;
$$;
