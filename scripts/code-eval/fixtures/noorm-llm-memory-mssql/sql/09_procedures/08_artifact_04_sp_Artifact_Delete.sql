CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Delete]
    @artifact_id INT,
    @agent_id    INT,
    @reason      NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;
    EXEC [dbo].[sp_Artifact_SetRelevance]
        @artifact_id          = @artifact_id,
        @new_relevance_status = 'deleted',
        @agent_id             = @agent_id,
        @reason               = @reason;
END
