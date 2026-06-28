CREATE OR ALTER PROCEDURE [dbo].[sp_Memory_Restore]
    @memory_id INT,
    @agent_id  INT,
    @reason    NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;
    EXEC [dbo].[sp_Memory_SetRelevance]
        @memory_id            = @memory_id,
        @new_relevance_status = 'active',
        @agent_id             = @agent_id,
        @reason               = @reason;
END
