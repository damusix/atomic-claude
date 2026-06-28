-- =============================================================================
-- Milestone procedures
-- -----------------------------------------------------------------------------
-- sp_Milestone_Create          — insert with default not-started/active
-- sp_Milestone_Update          — modify metadata (status changes use SetTracking / SetRelevance)
-- sp_Milestone_SetTracking     — gated tracking_status change + StateTransition log
-- sp_Milestone_SetRelevance    — gated relevance_status change + StateTransition log
-- sp_Milestone_Delete          — soft-delete via SetRelevance + cascade soft-delete attached Notes
-- sp_Milestone_Restore         — soft-undelete via SetRelevance to active
-- sp_Milestone_Close           — done + superseded + abandon all open child Tasks
-- sp_Milestone_Attach_Project  — idempotent insert into Project_Milestone
-- sp_Milestone_Detach_Project  — idempotent delete from Project_Milestone
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Create]
    @title         NVARCHAR(255),
    @content       NVARCHAR(MAX) = N'',
    @reason        NVARCHAR(255) = N'',
    @provenance_id INT           = 0,
    @agent_id      INT           = 0
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    INSERT INTO [dbo].[Milestone]
            ([tracking_status], [relevance_status], [provenance_id], [agent_id],
             [title], [content], [reason])
        VALUES
            ('not-started', 'active', @provenance_id, @agent_id,
             @title, @content, @reason);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [milestone_id];
END
