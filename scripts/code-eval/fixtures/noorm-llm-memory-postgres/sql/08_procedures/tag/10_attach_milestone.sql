-- Procedure: sp_Tag_Attach_Milestone
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Tag_Attach_Milestone"(
    p_tag_id INT,
    p_milestone_id INT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "Milestone_Tag" (tag_id, milestone_id)
        VALUES (p_tag_id, p_milestone_id)
        ON CONFLICT DO NOTHING;
END;
$$;
