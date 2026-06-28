-- Procedure: sp_Task_Depend
-- Source: tmp/llm-memory-db.pseudo  (STORED PROCEDURES section)

CREATE OR REPLACE PROCEDURE "sp_Task_Depend"(
    p_milestone_id INT,
    p_task_no INT,
    p_dep_milestone_id INT,
    p_dep_task_no INT,
    p_dependency_verb TEXT,
    p_reason TEXT
)
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_milestone_id = p_dep_milestone_id AND p_task_no = p_dep_task_no THEN
        RAISE EXCEPTION 'task cannot depend on itself' USING ERRCODE = '22023';
    END IF;

    IF "fn_TaskDependencyWouldCycle"(p_milestone_id, p_task_no, p_dep_milestone_id, p_dep_task_no) THEN
        RAISE EXCEPTION 'dependency would create a cycle' USING ERRCODE = '23514';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM "DependencyVerb" WHERE dependency_verb = p_dependency_verb) THEN
        RAISE EXCEPTION 'unknown DependencyVerb: %', p_dependency_verb USING ERRCODE = '23503';
    END IF;

    INSERT INTO "Task_Dependency" (milestone_id, task_no, dep_milestone_id, dep_task_no, dependency_verb, reason)
        VALUES (p_milestone_id, p_task_no, p_dep_milestone_id, p_dep_task_no, p_dependency_verb, p_reason)
        ON CONFLICT DO NOTHING;
END;
$$;
