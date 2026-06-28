-- =============================================================================
-- Agent procedures
-- -----------------------------------------------------------------------------
-- sp_Agent_Create   — insert and return new agent_id
-- sp_Agent_Update   — modify name/description (sentinel-protected)
-- sp_Agent_Delete   — reassign references to sentinel 0, then hard-delete
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Agent_Create]
    @name        NVARCHAR(255),
    @description NVARCHAR(255) = N''
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    INSERT INTO [dbo].[Agent] ([name], [description])
        VALUES (@name, @description);
    SET @new_id = SCOPE_IDENTITY();

    SELECT @new_id AS [agent_id];
END
