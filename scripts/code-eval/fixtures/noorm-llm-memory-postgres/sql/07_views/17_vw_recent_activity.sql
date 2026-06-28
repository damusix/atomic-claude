-- View: vw_Recent_Activity
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Cross-entity stream of recent changes. UNION ALL across each entity table
-- (created + updated branches where applicable) plus StateTransition for the
-- 'transitioned' action. Callers ORDER BY occurred_at DESC and LIMIT.
--
-- entity_id is INT for elevated entities. Tasks have a composite identity, so
-- entity_id is 0 for tasks and the composite key is exposed via the separate
-- milestone_id and task_no columns.
--
-- title_or_excerpt = COALESCE(title, LEFT(content, 80)) for entities with a
-- title; entities without a title (Note) fall through to the content excerpt;
-- entities with neither (Tag, Project) emit their name.

DROP VIEW IF EXISTS "vw_Recent_Activity" CASCADE;

CREATE VIEW "vw_Recent_Activity" AS
    -- ----- Memory: created -----
    SELECT
        'memory'::VARCHAR(32)                            AS entity_type,
        m.memory_id                                      AS entity_id,
        0                                                AS milestone_id,
        0                                                AS task_no,
        LEFT(m.content, 80)                              AS title_or_excerpt,
        m.agent_id,
        'created'::VARCHAR(32)                           AS action_type,
        m.created_at                                     AS occurred_at
    FROM "Memory" m

    UNION ALL

    -- ----- Memory: updated -----
    SELECT
        'memory'::VARCHAR(32),
        m.memory_id,
        0,
        0,
        LEFT(m.content, 80),
        m.agent_id,
        'updated'::VARCHAR(32),
        m.updated_at
    FROM "Memory" m
    WHERE m.updated_at <> m.created_at

    UNION ALL

    -- ----- Note: created -----
    SELECT
        'note'::VARCHAR(32),
        n.note_id,
        0,
        0,
        LEFT(n.content, 80),
        n.agent_id,
        'created'::VARCHAR(32),
        n.created_at
    FROM "Note" n

    UNION ALL

    -- ----- Note: updated -----
    SELECT
        'note'::VARCHAR(32),
        n.note_id,
        0,
        0,
        LEFT(n.content, 80),
        n.agent_id,
        'updated'::VARCHAR(32),
        n.updated_at
    FROM "Note" n
    WHERE n.updated_at <> n.created_at

    UNION ALL

    -- ----- Milestone: created -----
    SELECT
        'milestone'::VARCHAR(32),
        ms.milestone_id,
        ms.milestone_id,
        0,
        COALESCE(NULLIF(ms.title, ''), LEFT(ms.content, 80)),
        ms.agent_id,
        'created'::VARCHAR(32),
        ms.created_at
    FROM "Milestone" ms

    UNION ALL

    -- ----- Milestone: updated -----
    SELECT
        'milestone'::VARCHAR(32),
        ms.milestone_id,
        ms.milestone_id,
        0,
        COALESCE(NULLIF(ms.title, ''), LEFT(ms.content, 80)),
        ms.agent_id,
        'updated'::VARCHAR(32),
        ms.updated_at
    FROM "Milestone" ms
    WHERE ms.updated_at <> ms.created_at

    UNION ALL

    -- ----- Task: created -----
    SELECT
        'task'::VARCHAR(32),
        0,
        t.milestone_id,
        t.task_no,
        COALESCE(NULLIF(t.title, ''), LEFT(t.content, 80)),
        t.agent_id,
        'created'::VARCHAR(32),
        t.created_at
    FROM "Task" t

    UNION ALL

    -- ----- Task: updated -----
    SELECT
        'task'::VARCHAR(32),
        0,
        t.milestone_id,
        t.task_no,
        COALESCE(NULLIF(t.title, ''), LEFT(t.content, 80)),
        t.agent_id,
        'updated'::VARCHAR(32),
        t.updated_at
    FROM "Task" t
    WHERE t.updated_at <> t.created_at

    UNION ALL

    -- ----- Artifact: created -----
    SELECT
        'artifact'::VARCHAR(32),
        a.artifact_id,
        0,
        0,
        COALESCE(NULLIF(a.title, ''), a.filepath),
        a.agent_id,
        'created'::VARCHAR(32),
        a.created_at
    FROM "Artifact" a

    UNION ALL

    -- ----- Artifact: updated -----
    SELECT
        'artifact'::VARCHAR(32),
        a.artifact_id,
        0,
        0,
        COALESCE(NULLIF(a.title, ''), a.filepath),
        a.agent_id,
        'updated'::VARCHAR(32),
        a.updated_at
    FROM "Artifact" a
    WHERE a.updated_at <> a.created_at

    UNION ALL

    -- ----- Tag: created (tags do not have a relevance lifecycle of their own) -----
    SELECT
        'tag'::VARCHAR(32),
        tg.tag_id,
        0,
        0,
        tg.name,
        tg.agent_id,
        'created'::VARCHAR(32),
        tg.created_at
    FROM "Tag" tg

    UNION ALL

    -- ----- Project: created -----
    SELECT
        'project'::VARCHAR(32),
        p.project_id,
        0,
        0,
        p.name,
        p.agent_id,
        'created'::VARCHAR(32),
        p.created_at
    FROM "Project" p

    UNION ALL

    -- ----- StateTransition: transitioned -----
    -- entity_type derived from the discriminator prefix.
    -- entity_id / milestone_id / task_no come from the matching subtype join.
    SELECT
        CASE
            WHEN st.state_transition_type IN ('milestone-tracking', 'milestone-relevance')
                THEN 'milestone'
            WHEN st.state_transition_type = 'task-tracking'    THEN 'task'
            WHEN st.state_transition_type = 'memory-relevance' THEN 'memory'
            WHEN st.state_transition_type = 'note-relevance'   THEN 'note'
            WHEN st.state_transition_type = 'artifact-relevance' THEN 'artifact'
            ELSE 'unknown'
        END::VARCHAR(32) AS entity_type,
        COALESCE(memst.memory_id, nst.note_id, ast.artifact_id, mst.milestone_id, 0) AS entity_id,
        COALESCE(mst.milestone_id, tst.milestone_id, 0) AS milestone_id,
        COALESCE(tst.task_no, 0) AS task_no,
        st.reason AS title_or_excerpt,
        st.agent_id,
        'transitioned'::VARCHAR(32),
        st.occurred_at
    FROM "StateTransition" st
    LEFT JOIN "Milestone_StateTransition" mst   ON mst.transition_id   = st.transition_id
    LEFT JOIN "Task_StateTransition"      tst   ON tst.transition_id   = st.transition_id
    LEFT JOIN "Memory_StateTransition"    memst ON memst.transition_id = st.transition_id
    LEFT JOIN "Note_StateTransition"      nst   ON nst.transition_id   = st.transition_id
    LEFT JOIN "Artifact_StateTransition"  ast   ON ast.transition_id   = st.transition_id;
