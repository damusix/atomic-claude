-- =============================================================================
-- fn_NoteMatchesSubtype (note_id, note_type) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 when the given note_id has a subtype row in the table that
-- matches its declared note_type:
--   'project'   -> Project_Note
--   'milestone' -> Milestone_Note
--   'task'      -> Task_Note
-- Returns 0 if missing, mismatched, or note_type unrecognized.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_NoteMatchesSubtype](
    @note_id   INT,
    @note_type VARCHAR(32)
)
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @matches BIT = 0;

    IF @note_type = 'project'
    BEGIN
        IF EXISTS (SELECT 1 FROM [dbo].[Project_Note] WHERE [note_id] = @note_id)
            SET @matches = 1;
    END
    ELSE IF @note_type = 'milestone'
    BEGIN
        IF EXISTS (SELECT 1 FROM [dbo].[Milestone_Note] WHERE [note_id] = @note_id)
            SET @matches = 1;
    END
    ELSE IF @note_type = 'task'
    BEGIN
        IF EXISTS (SELECT 1 FROM [dbo].[Task_Note] WHERE [note_id] = @note_id)
            SET @matches = 1;
    END;

    RETURN @matches;
END;
