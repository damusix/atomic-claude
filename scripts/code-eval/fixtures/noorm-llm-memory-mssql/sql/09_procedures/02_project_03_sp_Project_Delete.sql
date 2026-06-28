CREATE OR ALTER PROCEDURE [dbo].[sp_Project_Delete]
    @project_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF @project_id = 0
    BEGIN
        RAISERROR('Project(0) is the sentinel and cannot be modified or deleted.', 16, 1);
        RETURN;
    END

    -- Reassign provenance_id back to sentinel 0 on every entity that pointed here
    UPDATE [dbo].[Note]      SET [provenance_id] = 0 WHERE [provenance_id] = @project_id;
    UPDATE [dbo].[Tag]       SET [provenance_id] = 0 WHERE [provenance_id] = @project_id;
    UPDATE [dbo].[Memory]    SET [provenance_id] = 0 WHERE [provenance_id] = @project_id;
    UPDATE [dbo].[Artifact]  SET [provenance_id] = 0 WHERE [provenance_id] = @project_id;
    UPDATE [dbo].[Milestone] SET [provenance_id] = 0 WHERE [provenance_id] = @project_id;

    -- Soft-delete every Note attached via Project_Note (skip already-deleted ones
    -- and reject paths the state machine would refuse).
    DECLARE @note_id INT;
    DECLARE @cur_rs VARCHAR(32);

    DECLARE note_cur CURSOR LOCAL FAST_FORWARD FOR
        SELECT pn.[note_id]
        FROM [dbo].[Project_Note] pn
        WHERE pn.[project_id] = @project_id;

    OPEN note_cur;
    FETCH NEXT FROM note_cur INTO @note_id;
    WHILE @@FETCH_STATUS = 0
    BEGIN
        SELECT @cur_rs = [relevance_status]
        FROM [dbo].[Note]
        WHERE [note_id] = @note_id;

        IF @cur_rs <> 'deleted'
           AND [dbo].[fn_IsRelevanceTransitionAllowed](@cur_rs, 'deleted') = 1
        BEGIN
            EXEC [dbo].[sp_Note_Delete]
                @note_id  = @note_id,
                @agent_id = 0,
                @reason   = N'Cascade soft-delete from sp_Project_Delete';
        END

        FETCH NEXT FROM note_cur INTO @note_id;
    END
    CLOSE note_cur;
    DEALLOCATE note_cur;

    DELETE FROM [dbo].[Project] WHERE [project_id] = @project_id;
END
