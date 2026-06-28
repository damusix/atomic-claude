CREATE OR ALTER PROCEDURE [dbo].[sp_Note_Delete]
    @note_id  INT,
    @agent_id INT,
    @reason   NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;
    EXEC [dbo].[sp_Note_SetRelevance]
        @note_id              = @note_id,
        @new_relevance_status = 'deleted',
        @agent_id             = @agent_id,
        @reason               = @reason;
END
