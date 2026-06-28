-- Function: fn_TaskDependencyWouldCycle
-- Source: tmp/llm-memory-db.pseudo  (SCALAR FUNCTIONS section)
--
-- Recursive walk of Task_Dependency starting at (dep_milestone_id, dep_task_no),
-- following dep edges. Returns true if (milestone_id, task_no) is reachable —
-- inserting this dependency would create a cycle.
-- Called by sp_Task_Depend before INSERT. Rejects the insert if true.

CREATE OR REPLACE FUNCTION "fn_TaskDependencyWouldCycle"(
    p_milestone_id     INT,
    p_task_no          INT,
    p_dep_milestone_id INT,
    p_dep_task_no      INT
)
RETURNS BOOLEAN
LANGUAGE plpgsql STABLE
AS $$
DECLARE
    cycle_found BOOLEAN;
BEGIN
    WITH RECURSIVE walk(milestone_id, task_no) AS (
        SELECT p_dep_milestone_id, p_dep_task_no
        UNION
        SELECT td.dep_milestone_id, td.dep_task_no
        FROM "Task_Dependency" td
        INNER JOIN walk w
            ON td.milestone_id = w.milestone_id
           AND td.task_no      = w.task_no
    )
    SELECT EXISTS (
        SELECT 1
        FROM walk
        WHERE milestone_id = p_milestone_id
          AND task_no      = p_task_no
    )
    INTO cycle_found;

    RETURN cycle_found;
END;
$$;
