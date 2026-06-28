-- View: vw_Tag
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- One row per tag-entity attachment. UNION ALL across the 5 *_Tag join tables.
-- Tags with zero attachments are absent (inner join semantics).
-- Unused entity columns default to 0 to satisfy the project's no-NULL invariant.

DROP VIEW IF EXISTS "vw_Tag" CASCADE;

CREATE VIEW "vw_Tag" AS
    SELECT
        t.tag_id,
        t.name,
        t.description,
        t.reason,
        t.provenance_id,
        'project'::VARCHAR(32) AS relation_type,
        pt.project_id          AS project_id,
        0                      AS memory_id,
        0                      AS artifact_id,
        0                      AS milestone_id,
        0                      AS task_no,
        pt.created_at          AS created_at
    FROM "Tag" t
    INNER JOIN "Project_Tag" pt ON pt.tag_id = t.tag_id

    UNION ALL

    SELECT
        t.tag_id,
        t.name,
        t.description,
        t.reason,
        t.provenance_id,
        'memory'::VARCHAR(32)  AS relation_type,
        0                      AS project_id,
        mt.memory_id           AS memory_id,
        0                      AS artifact_id,
        0                      AS milestone_id,
        0                      AS task_no,
        mt.created_at          AS created_at
    FROM "Tag" t
    INNER JOIN "Memory_Tag" mt ON mt.tag_id = t.tag_id

    UNION ALL

    SELECT
        t.tag_id,
        t.name,
        t.description,
        t.reason,
        t.provenance_id,
        'artifact'::VARCHAR(32) AS relation_type,
        0                       AS project_id,
        0                       AS memory_id,
        art.artifact_id         AS artifact_id,
        0                       AS milestone_id,
        0                       AS task_no,
        art.created_at          AS created_at
    FROM "Tag" t
    INNER JOIN "Artifact_Tag" art ON art.tag_id = t.tag_id

    UNION ALL

    SELECT
        t.tag_id,
        t.name,
        t.description,
        t.reason,
        t.provenance_id,
        'milestone'::VARCHAR(32) AS relation_type,
        0                        AS project_id,
        0                        AS memory_id,
        0                        AS artifact_id,
        mst.milestone_id         AS milestone_id,
        0                        AS task_no,
        mst.created_at           AS created_at
    FROM "Tag" t
    INNER JOIN "Milestone_Tag" mst ON mst.tag_id = t.tag_id

    UNION ALL

    SELECT
        t.tag_id,
        t.name,
        t.description,
        t.reason,
        t.provenance_id,
        'task'::VARCHAR(32) AS relation_type,
        0                   AS project_id,
        0                   AS memory_id,
        0                   AS artifact_id,
        tt.milestone_id     AS milestone_id,
        tt.task_no          AS task_no,
        tt.created_at       AS created_at
    FROM "Tag" t
    INNER JOIN "Task_Tag" tt ON tt.tag_id = t.tag_id;
