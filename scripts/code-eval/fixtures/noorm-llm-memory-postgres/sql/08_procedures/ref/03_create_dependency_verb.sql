-- Procedure: sp_Ref_Create_DependencyVerb
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Create_DependencyVerb"(
    p_dependency_verb TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO "DependencyVerb" (dependency_verb)
        VALUES (p_dependency_verb)
        ON CONFLICT DO NOTHING;
END;
$$;
