-- Function: fn_NextTaskNo
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Find the highest task_no in Task for this milestone_id.
-- If none exist, return 1. Otherwise return that number + 1.

CREATE OR REPLACE FUNCTION "fn_NextTaskNo"(p_milestone_id INT)
RETURNS INT
LANGUAGE sql STABLE
AS $$
    SELECT COALESCE(MAX(task_no), 0) + 1
    FROM "Task"
    WHERE milestone_id = p_milestone_id;
$$;
