-- =============================================================================
-- sp_Cleanup — TTL-based hard delete pass
-- -----------------------------------------------------------------------------
-- Hard-deletes soft-deleted entities (relevance_status = 'deleted') whose
-- updated_at is older than @ttl_days. Also clears the orphaned audit trail
-- and the join rows that point at deleted entities through NO-ACTION FKs.
--
-- Cascade map (per the schema's FK declarations):
--
--   StateTransition (basetype) -> *_StateTransition (subtype)  : CASCADE
--   *_StateTransition (subtype) -> entity                      : NO ACTION
--   Memory_Tag.tag_id -> Tag                                   : CASCADE
--   Memory_Tag.memory_id -> Memory                             : NO ACTION
--   Project_Memory.memory_id -> Memory                         : CASCADE
--   Project_Memory.project_id -> Project                       : NO ACTION
--   Related_Memory.memory_id -> Memory                         : CASCADE
--   Related_Memory.related_memory_id -> Memory                 : NO ACTION
--   Milestone_Artifact.milestone_id -> Milestone               : CASCADE
--   Milestone_Artifact.artifact_id -> Artifact                 : NO ACTION
--   Task_Artifact.(milestone_id, task_no) -> Task              : CASCADE
--   Task_Artifact.artifact_id -> Artifact                      : NO ACTION
--   Artifact_Tag.artifact_id -> Artifact                       : NO ACTION
--   Milestone_Tag.milestone_id -> Milestone                    : NO ACTION
--   Task_Tag.(milestone_id, task_no) -> Task                   : NO ACTION
--   Task_Dependency.(milestone_id, task_no) -> Task            : CASCADE
--   Task_Dependency.(dep_milestone_id, dep_task_no) -> Task    : NO ACTION
--   Project_Note / Milestone_Note / Task_Note (subtypes) -> Note : CASCADE
--
-- Strategy per entity (in order):
--   1. Capture transition_ids whose subtype rows reference doomed entities.
--   2. Explicitly delete every NO-ACTION join row that points at the doomed
--      entities. This unblocks the entity DELETE.
--   3. Hard-delete the entity rows. CASCADE then clears the rest.
--   4. Delete the captured StateTransition basetype rows. CASCADE then clears
--      the *_StateTransition subtype rows in a single, controlled pass.
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Cleanup]
    @ttl_days INT
AS
BEGIN
    SET NOCOUNT ON;

    DECLARE @cutoff DATETIME2 = DATEADD(DAY, -@ttl_days, SYSUTCDATETIME());

    -- Counters returned to the caller
    DECLARE @memories_removed   INT = 0;
    DECLARE @notes_removed      INT = 0;
    DECLARE @artifacts_removed  INT = 0;
    DECLARE @milestones_removed INT = 0;
    DECLARE @transitions_removed INT = 0;

    BEGIN TRANSACTION;

        -- =====================================================================
        -- Working sets: doomed entity ids and their transition ids
        -- =====================================================================
        CREATE TABLE #doomed_memories   ([memory_id] INT NOT NULL PRIMARY KEY);
        CREATE TABLE #doomed_notes      ([note_id] INT NOT NULL PRIMARY KEY);
        CREATE TABLE #doomed_artifacts  ([artifact_id] INT NOT NULL PRIMARY KEY);
        CREATE TABLE #doomed_milestones ([milestone_id] INT NOT NULL PRIMARY KEY);
        CREATE TABLE #expired_transitions ([transition_id] INT NOT NULL PRIMARY KEY);

        INSERT INTO #doomed_memories ([memory_id])
            SELECT [memory_id] FROM [dbo].[Memory]
            WHERE [relevance_status] = 'deleted' AND [updated_at] < @cutoff;

        INSERT INTO #doomed_notes ([note_id])
            SELECT [note_id] FROM [dbo].[Note]
            WHERE [relevance_status] = 'deleted' AND [updated_at] < @cutoff;

        INSERT INTO #doomed_artifacts ([artifact_id])
            SELECT [artifact_id] FROM [dbo].[Artifact]
            WHERE [relevance_status] = 'deleted' AND [updated_at] < @cutoff;

        INSERT INTO #doomed_milestones ([milestone_id])
            SELECT [milestone_id] FROM [dbo].[Milestone]
            WHERE [relevance_status] = 'deleted' AND [updated_at] < @cutoff;

        -- =====================================================================
        -- Capture transition ids BEFORE entity deletes (subtype rows die with
        -- the basetype cascade, so we'd lose linkage if we deleted entities
        -- first).
        -- =====================================================================
        INSERT INTO #expired_transitions ([transition_id])
            SELECT mst.[transition_id]
            FROM [dbo].[Memory_StateTransition] mst
            INNER JOIN #doomed_memories d ON d.[memory_id] = mst.[memory_id]
            WHERE NOT EXISTS (
                SELECT 1 FROM #expired_transitions et
                WHERE et.[transition_id] = mst.[transition_id]
            );

        INSERT INTO #expired_transitions ([transition_id])
            SELECT nst.[transition_id]
            FROM [dbo].[Note_StateTransition] nst
            INNER JOIN #doomed_notes d ON d.[note_id] = nst.[note_id]
            WHERE NOT EXISTS (
                SELECT 1 FROM #expired_transitions et
                WHERE et.[transition_id] = nst.[transition_id]
            );

        INSERT INTO #expired_transitions ([transition_id])
            SELECT ast.[transition_id]
            FROM [dbo].[Artifact_StateTransition] ast
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = ast.[artifact_id]
            WHERE NOT EXISTS (
                SELECT 1 FROM #expired_transitions et
                WHERE et.[transition_id] = ast.[transition_id]
            );

        INSERT INTO #expired_transitions ([transition_id])
            SELECT mst.[transition_id]
            FROM [dbo].[Milestone_StateTransition] mst
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = mst.[milestone_id]
            WHERE NOT EXISTS (
                SELECT 1 FROM #expired_transitions et
                WHERE et.[transition_id] = mst.[transition_id]
            );

        -- Task transitions live under doomed milestones (Task cascades from
        -- Milestone, so child tasks die with the milestone DELETE).
        INSERT INTO #expired_transitions ([transition_id])
            SELECT tst.[transition_id]
            FROM [dbo].[Task_StateTransition] tst
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = tst.[milestone_id]
            WHERE NOT EXISTS (
                SELECT 1 FROM #expired_transitions et
                WHERE et.[transition_id] = tst.[transition_id]
            );

        -- =====================================================================
        -- Memory cleanup
        -- =====================================================================
        DELETE mt FROM [dbo].[Memory_Tag] mt
            INNER JOIN #doomed_memories d ON d.[memory_id] = mt.[memory_id];

        -- Related_Memory cascades from memory_id but NOT from related_memory_id.
        DELETE rm FROM [dbo].[Related_Memory] rm
            INNER JOIN #doomed_memories d ON d.[memory_id] = rm.[related_memory_id];

        DELETE mst FROM [dbo].[Memory_StateTransition] mst
            INNER JOIN #doomed_memories d ON d.[memory_id] = mst.[memory_id];

        DELETE m FROM [dbo].[Memory] m
            INNER JOIN #doomed_memories d ON d.[memory_id] = m.[memory_id];
        SET @memories_removed = @@ROWCOUNT;

        -- =====================================================================
        -- Note cleanup
        -- =====================================================================
        DELETE nst FROM [dbo].[Note_StateTransition] nst
            INNER JOIN #doomed_notes d ON d.[note_id] = nst.[note_id];

        DELETE n FROM [dbo].[Note] n
            INNER JOIN #doomed_notes d ON d.[note_id] = n.[note_id];
        SET @notes_removed = @@ROWCOUNT;

        -- =====================================================================
        -- Artifact cleanup
        -- =====================================================================
        DELETE at_t FROM [dbo].[Artifact_Tag] at_t
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = at_t.[artifact_id];

        DELETE ma FROM [dbo].[Milestone_Artifact] ma
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = ma.[artifact_id];

        DELETE ta FROM [dbo].[Task_Artifact] ta
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = ta.[artifact_id];

        DELETE ast FROM [dbo].[Artifact_StateTransition] ast
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = ast.[artifact_id];

        DELETE a FROM [dbo].[Artifact] a
            INNER JOIN #doomed_artifacts d ON d.[artifact_id] = a.[artifact_id];
        SET @artifacts_removed = @@ROWCOUNT;

        -- =====================================================================
        -- Milestone cleanup
        -- =====================================================================
        -- Milestone_Tag is NO ACTION on the milestone side.
        DELETE mt FROM [dbo].[Milestone_Tag] mt
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = mt.[milestone_id];

        -- Task_Tag and Task_Dependency hang off Tasks under doomed milestones.
        -- Both are NO ACTION on the Task side, so explicit deletes are required
        -- before the Milestone DELETE cascades to Task.
        DELETE tt FROM [dbo].[Task_Tag] tt
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = tt.[milestone_id];

        -- Task_Dependency rows whose dep side points at a task under a doomed
        -- milestone (the same-side cascade handles rows on the milestone's own
        -- tasks; this clears the cross-milestone references).
        DELETE td FROM [dbo].[Task_Dependency] td
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = td.[dep_milestone_id];

        -- Task_StateTransition is NO ACTION on the Task side.
        DELETE tst FROM [dbo].[Task_StateTransition] tst
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = tst.[milestone_id];

        -- Milestone_StateTransition is NO ACTION on the Milestone side.
        DELETE mst FROM [dbo].[Milestone_StateTransition] mst
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = mst.[milestone_id];

        DELETE m FROM [dbo].[Milestone] m
            INNER JOIN #doomed_milestones d ON d.[milestone_id] = m.[milestone_id];
        SET @milestones_removed = @@ROWCOUNT;

        -- =====================================================================
        -- Drop the now-orphaned StateTransition basetype rows. The subtype
        -- rows have already been deleted explicitly above (the basetype cascade
        -- would have nothing left to remove).
        -- =====================================================================
        DELETE st FROM [dbo].[StateTransition] st
            INNER JOIN #expired_transitions et ON et.[transition_id] = st.[transition_id];
        SET @transitions_removed = @@ROWCOUNT;

        DROP TABLE #doomed_memories;
        DROP TABLE #doomed_notes;
        DROP TABLE #doomed_artifacts;
        DROP TABLE #doomed_milestones;
        DROP TABLE #expired_transitions;
    COMMIT TRANSACTION;

    SELECT
        @memories_removed    AS [memories_removed],
        @notes_removed       AS [notes_removed],
        @artifacts_removed   AS [artifacts_removed],
        @milestones_removed  AS [milestones_removed],
        @transitions_removed AS [transitions_removed];
END
