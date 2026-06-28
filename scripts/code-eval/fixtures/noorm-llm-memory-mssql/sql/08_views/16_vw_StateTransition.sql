-- =============================================================================
-- vw_StateTransition
-- -----------------------------------------------------------------------------
-- Unified audit history. StateTransition LEFT JOIN to all 5 *_StateTransition
-- subtype tables, with COALESCE on entity columns. Exclusivity is guaranteed
-- by the INSTEAD OF triggers in 09_triggers, so exactly one subtype matches
-- per row.
--
-- Columns:
--   transition_id, state_transition_type, agent_id,
--   from_status, to_status, reason, occurred_at, created_at,
--   milestone_id (set for milestone-tracking, milestone-relevance, task-tracking),
--   task_no      (set for task-tracking),
--   memory_id    (set for memory-relevance),
--   note_id      (set for note-relevance),
--   artifact_id  (set for artifact-relevance).
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_StateTransition]
AS
SELECT
    st.[transition_id],
    st.[state_transition_type],
    st.[agent_id],
    st.[from_status],
    st.[to_status],
    st.[reason],
    st.[occurred_at],
    st.[created_at],
    COALESCE(mst.[milestone_id], tst.[milestone_id], 0) AS [milestone_id],
    COALESCE(tst.[task_no], 0)                          AS [task_no],
    COALESCE(memst.[memory_id], 0)                      AS [memory_id],
    COALESCE(nst.[note_id], 0)                          AS [note_id],
    COALESCE(ast.[artifact_id], 0)                      AS [artifact_id]
FROM [dbo].[StateTransition] st
LEFT JOIN [dbo].[Milestone_StateTransition] mst   ON mst.[transition_id]   = st.[transition_id]
LEFT JOIN [dbo].[Task_StateTransition]      tst   ON tst.[transition_id]   = st.[transition_id]
LEFT JOIN [dbo].[Memory_StateTransition]    memst ON memst.[transition_id] = st.[transition_id]
LEFT JOIN [dbo].[Note_StateTransition]      nst   ON nst.[transition_id]   = st.[transition_id]
LEFT JOIN [dbo].[Artifact_StateTransition]  ast   ON ast.[transition_id]   = st.[transition_id];
