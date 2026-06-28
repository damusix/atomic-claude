-- View: vw_Milestone_Stats
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- One row per milestone with rolled-up counts. Task-level relations
-- (artifacts, notes, tags, dependencies) are aggregated to the milestone,
-- not exposed per-task.
--
-- Each rollup is an independent scalar subquery to avoid join multiplication.

DROP VIEW IF EXISTS "vw_Milestone_Stats" CASCADE;

CREATE VIEW "vw_Milestone_Stats" AS
    SELECT
        m.milestone_id,
        m.title,
        m.content,
        m.reason,
        m.tracking_status,
        m.relevance_status,
        m.provenance_id,
        (
            SELECT COUNT(*)::INT FROM "Task" t
            WHERE t.milestone_id = m.milestone_id
        ) AS total_tasks,
        (
            SELECT COUNT(*)::INT FROM "Task" t
            WHERE t.milestone_id = m.milestone_id
              AND t.tracking_status NOT IN ('done', 'abandoned')
        ) AS open_tasks,
        (
            SELECT COUNT(*)::INT FROM "Task" t
            WHERE t.milestone_id = m.milestone_id
              AND t.tracking_status = 'done'
        ) AS done_tasks,
        (
            SELECT COUNT(*)::INT FROM "Task" t
            WHERE t.milestone_id = m.milestone_id
              AND t.tracking_status = 'abandoned'
        ) AS abandoned_tasks,
        (
            SELECT COUNT(*)::INT FROM "Task" t
            WHERE t.milestone_id = m.milestone_id
              AND EXISTS (
                  SELECT 1
                  FROM "Task_Dependency" td
                  INNER JOIN "Task" dt
                      ON dt.milestone_id = td.dep_milestone_id
                     AND dt.task_no      = td.dep_task_no
                  WHERE td.milestone_id    = t.milestone_id
                    AND td.task_no         = t.task_no
                    AND td.dependency_verb = 'blocks'
                    AND dt.tracking_status <> 'done'
              )
        ) AS blocked_tasks,
        (
            SELECT COUNT(*)::INT FROM "Milestone_Artifact" ma
            WHERE ma.milestone_id = m.milestone_id
        )
        +
        (
            SELECT COUNT(*)::INT FROM "Task_Artifact" ta
            WHERE ta.milestone_id = m.milestone_id
        ) AS total_artifacts,
        (
            SELECT COUNT(*)::INT FROM "Milestone_Note" mn
            WHERE mn.milestone_id = m.milestone_id
        )
        +
        (
            SELECT COUNT(*)::INT FROM "Task_Note" tn
            WHERE tn.milestone_id = m.milestone_id
        ) AS total_notes,
        (
            SELECT COUNT(*)::INT FROM "Milestone_Tag" mt
            WHERE mt.milestone_id = m.milestone_id
        )
        +
        (
            SELECT COUNT(*)::INT FROM "Task_Tag" tgt
            WHERE tgt.milestone_id = m.milestone_id
        ) AS total_tags,
        (
            SELECT COUNT(*)::INT FROM "Task_Dependency" td
            WHERE td.milestone_id     = m.milestone_id
               OR td.dep_milestone_id = m.milestone_id
        ) AS total_dependencies,
        (
            SELECT COUNT(*)::INT FROM "Project_Milestone" pm
            WHERE pm.milestone_id = m.milestone_id
        ) AS project_count,
        m.created_at,
        m.updated_at
    FROM "Milestone" m;
