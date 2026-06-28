-- Procedure: sp_Tag_Attach_Memory
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Attach_Memory"(
    p_tag_id INT,
    p_memory_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Memory_Tag" (tag_id, memory_id)
        VALUES (p_tag_id, p_memory_id)
        ON CONFLICT DO NOTHING;
END;
$$;
