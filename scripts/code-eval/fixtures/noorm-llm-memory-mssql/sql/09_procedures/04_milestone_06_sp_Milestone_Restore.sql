CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Restore]
    @milestone_id INT,
    @agent_id     INT,
    @reason       NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;

    EXEC [dbo].[sp_Milestone_SetRelevance]
        @milestone_id         = @milestone_id,
        @new_relevance_status = 'active',
        @agent_id             = @agent_id,
        @reason               = @reason;
END
