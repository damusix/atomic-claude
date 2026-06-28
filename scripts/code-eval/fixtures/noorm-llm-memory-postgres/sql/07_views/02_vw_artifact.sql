-- View: vw_Artifact
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- One row per artifact-entity attachment. UNION ALL across the 2 *_Artifact join tables.
-- Artifacts with zero attachments are absent (inner join semantics).
-- Tag relations are intentionally excluded — query vw_Tag with relation_type = 'artifact'.

DROP VIEW IF EXISTS "vw_Artifact" CASCADE;

CREATE VIEW "vw_Artifact" AS
    SELECT
        a.artifact_id,
        a.title,
        a.description,
        a.filepath,
        a.reason,
        a.relevance_status,
        a.provenance_id,
        'milestone'::VARCHAR(32) AS relation_type,
        ma.milestone_id          AS milestone_id,
        0                        AS task_no,
        ma.created_at            AS created_at
    FROM "Artifact" a
    INNER JOIN "Milestone_Artifact" ma ON ma.artifact_id = a.artifact_id

    UNION ALL

    SELECT
        a.artifact_id,
        a.title,
        a.description,
        a.filepath,
        a.reason,
        a.relevance_status,
        a.provenance_id,
        'task'::VARCHAR(32) AS relation_type,
        ta.milestone_id     AS milestone_id,
        ta.task_no          AS task_no,
        ta.created_at       AS created_at
    FROM "Artifact" a
    INNER JOIN "Task_Artifact" ta ON ta.artifact_id = a.artifact_id;
