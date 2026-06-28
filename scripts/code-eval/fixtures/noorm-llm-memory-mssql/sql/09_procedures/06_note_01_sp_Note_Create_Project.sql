-- =============================================================================
-- Note procedures
-- -----------------------------------------------------------------------------
-- Notes are exclusive subtypes: each Note row pairs with exactly one of
-- Project_Note / Milestone_Note / Task_Note. Each Create variant inserts the
-- Note basetype + the matching subtype row in a single transaction so the
-- exclusivity constraint holds by construction. The triggers in 09_triggers
-- enforce that note_type matches the chosen subtype on every write.
--
-- sp_Note_Create_Project    — note_type='project'   + Project_Note
-- sp_Note_Create_Milestone  — note_type='milestone' + Milestone_Note
-- sp_Note_Create_Task       — note_type='task'      + Task_Note(milestone,task)
-- sp_Note_Update            — modify content/reason (subtype + type immutable)
-- sp_Note_SetRelevance      — gated state-machine transition + audit row
-- sp_Note_Delete            — wraps SetRelevance to 'deleted'
-- sp_Note_Restore           — wraps SetRelevance to 'active'
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Note_Create_Project]
    @content       NVARCHAR(MAX),
    @reason        NVARCHAR(255) = N'',
    @provenance_id INT           = 0,
    @agent_id      INT           = 0,
    @project_id    INT
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    BEGIN TRANSACTION;
        INSERT INTO [dbo].[Note]
                ([note_type], [relevance_status], [provenance_id], [agent_id],
                 [content], [reason])
            VALUES
                ('project', 'active', @provenance_id, @agent_id,
                 @content, @reason);
        SET @new_id = SCOPE_IDENTITY();

        INSERT INTO [dbo].[Project_Note] ([note_id], [project_id])
            VALUES (@new_id, @project_id);
    COMMIT TRANSACTION;

    SELECT @new_id AS [note_id];
END
