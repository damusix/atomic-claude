-- View: vw_Note
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Notes are exclusive subtypes — each note row owns exactly one of the three
-- subtype rows (Project_Note, Milestone_Note, Task_Note). Left-join all three
-- and COALESCE the entity columns to 0 so callers can switch on note_type.

DROP VIEW IF EXISTS "vw_Note" CASCADE;

CREATE VIEW "vw_Note" AS
    SELECT
        n.note_id,
        n.note_type,
        n.content,
        n.reason,
        n.relevance_status,
        n.provenance_id,
        COALESCE(pn.project_id, 0)                          AS project_id,
        COALESCE(mn.milestone_id, tn.milestone_id, 0)       AS milestone_id,
        COALESCE(tn.task_no, 0)                             AS task_no,
        n.agent_id,
        n.created_at,
        n.updated_at
    FROM "Note" n
    LEFT JOIN "Project_Note"   pn ON pn.note_id = n.note_id
    LEFT JOIN "Milestone_Note" mn ON mn.note_id = n.note_id
    LEFT JOIN "Task_Note"      tn ON tn.note_id = n.note_id;
