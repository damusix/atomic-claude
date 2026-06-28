-- =============================================================================
-- Artifact procedures
-- -----------------------------------------------------------------------------
-- sp_Artifact_Create            — insert (active)
-- sp_Artifact_Update            — modify title/description/filepath/reason
-- sp_Artifact_SetRelevance      — gated state-machine transition + audit row
-- sp_Artifact_Delete            — wraps SetRelevance to 'deleted'
-- sp_Artifact_Restore           — wraps SetRelevance to 'active'
-- sp_Artifact_Attach_Milestone  — idempotent insert into Milestone_Artifact
-- sp_Artifact_Detach_Milestone  — silent delete from Milestone_Artifact
-- sp_Artifact_Attach_Task       — idempotent insert into Task_Artifact
-- sp_Artifact_Detach_Task       — silent delete from Task_Artifact
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Create]
    @title         NVARCHAR(255),
    @description   NVARCHAR(255) = N'',
    @filepath      NVARCHAR(255) = N'',
    @reason        NVARCHAR(255) = N'',
    @provenance_id INT           = 0,
    @agent_id      INT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    INSERT INTO [dbo].[Artifact]
            ([relevance_status], [provenance_id], [agent_id],
             [title], [description], [filepath], [reason])
        VALUES
            ('active', @provenance_id, @agent_id,
             @title, @description, @filepath, @reason);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [artifact_id];
END
