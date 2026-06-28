-- View: vw_Task_Backlog
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Open tasks under active milestones, ordered by milestone_id, task_no.
-- is_blocked is true when at least one Task_Dependency edge with verb 'blocks'
-- points at a dependency task that has not yet reached 'done'.

DROP VIEW IF EXISTS "vw_Task_Backlog" CASCADE;

CREATE VIEW "vw_Task_Backlog" AS
    SELECT
        t.milestone_id,
        t.task_no,
        t.title,
        t.content,
        t.tracking_status,
        t.agent_id,
        EXISTS (
            SELECT 1
            FROM "Task_Dependency" td
            INNER JOIN "Task" dt
                ON dt.milestone_id = td.dep_milestone_id
               AND dt.task_no      = td.dep_task_no
            WHERE td.milestone_id    = t.milestone_id
              AND td.task_no         = t.task_no
              AND td.dependency_verb = 'blocks'
              AND dt.tracking_status <> 'done'
        ) AS is_blocked,
        t.created_at,
        t.updated_at
    FROM "Task" t
    INNER JOIN "Milestone" m ON m.milestone_id = t.milestone_id
    WHERE m.relevance_status = 'active'
      AND t.tracking_status NOT IN ('done', 'abandoned')
    ORDER BY t.milestone_id, t.task_no;
