-- Procedure: sp_Project_Update
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Project_Update"(
    p_project_id INT,
    p_name TEXT,
    p_filepath TEXT,
    p_git_repo TEXT,
    p_main_branch TEXT,
    p_git_url TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_project_id = 0 THEN
        RAISE EXCEPTION 'Sentinel project_id=0 is immutable' USING ERRCODE = '23514';
    END IF;

    UPDATE "Project"
        SET name = p_name,
            filepath = p_filepath,
            git_repo = p_git_repo,
            main_branch = p_main_branch,
            git_url = p_git_url,
            updated_at = NOW()
        WHERE project_id = p_project_id;
END;
$$;
