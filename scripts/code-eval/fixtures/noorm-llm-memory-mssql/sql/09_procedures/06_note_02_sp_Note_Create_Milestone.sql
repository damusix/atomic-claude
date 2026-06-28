CREATE OR ALTER PROCEDURE [dbo].[sp_Note_Create_Milestone]
    @content       NVARCHAR(MAX),
    @reason        NVARCHAR(255) = N'',
    @provenance_id INT           = 0,
    @agent_id      INT           = 0,
    @milestone_id  INT
AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @new_id INT;

    BEGIN TRANSACTION;
        INSERT INTO [dbo].[Note]
                ([note_type], [relevance_status], [provenance_id], [agent_id],
                 [content], [reason])
            VALUES
                ('milestone', 'active', @provenance_id, @agent_id,
                 @content, @reason);
        SET @new_id = SCOPE_IDENTITY();

        INSERT INTO [dbo].[Milestone_Note] ([note_id], [milestone_id])
            VALUES (@new_id, @milestone_id);
    COMMIT TRANSACTION;

    SELECT @new_id AS [note_id];
END
