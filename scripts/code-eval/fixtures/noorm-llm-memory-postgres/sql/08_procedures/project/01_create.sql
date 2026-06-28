-- Procedure: sp_Project_Create
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

DROP FUNCTION IF EXISTS "sp_Project_Create"(
    p_name TEXT,
    p_filepath TEXT,
    p_git_repo TEXT,
    p_main_branch TEXT,
    p_git_url TEXT,
    p_agent_id INT
) CASCADE;

CREATE OR REPLACE FUNCTION "sp_Project_Create"(
    p_name TEXT,
    p_filepath TEXT,
    p_git_repo TEXT,
    p_main_branch TEXT,
    p_git_url TEXT,
    p_agent_id INT
)
RETURNS TABLE(project_id INT)
LANGUAGE plpgsql
AS $$
DECLARE
    v_project_id INT;
BEGIN
    INSERT INTO "Project" (name, filepath, git_repo, main_branch, git_url, agent_id)
        VALUES (p_name, p_filepath, p_git_repo, p_main_branch, p_git_url, p_agent_id)
        RETURNING "Project".project_id INTO v_project_id;

    RETURN QUERY SELECT v_project_id;
END;
$$;
