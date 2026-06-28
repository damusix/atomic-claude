-- Procedure: sp_Ref_Delete_DependencyVerb
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Ref_Delete_DependencyVerb"(
    p_dependency_verb TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM "Task_Dependency" WHERE dependency_verb = p_dependency_verb) THEN
        RAISE EXCEPTION 'DependencyVerb % is in use and cannot be deleted', p_dependency_verb
            USING ERRCODE = '23503';
    END IF;

    DELETE FROM "DependencyVerb" WHERE dependency_verb = p_dependency_verb;
END;
$$;
