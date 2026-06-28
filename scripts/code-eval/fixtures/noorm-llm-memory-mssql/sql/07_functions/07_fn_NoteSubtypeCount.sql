-- =============================================================================
-- fn_NoteSubtypeCount (note_id) -> INT
-- -----------------------------------------------------------------------------
-- Counts how many of the three Note subtype tables (Project_Note,
-- Milestone_Note, Task_Note) hold a row for this note_id.
-- Valid data always returns 1. 0 = orphaned. >1 = exclusivity violation.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_NoteSubtypeCount](@note_id INT)
RETURNS INT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @count INT = 0;

    IF EXISTS (SELECT 1 FROM [dbo].[Project_Note]   WHERE [note_id] = @note_id)
        SET @count = @count + 1;

    IF EXISTS (SELECT 1 FROM [dbo].[Milestone_Note] WHERE [note_id] = @note_id)
        SET @count = @count + 1;

    IF EXISTS (SELECT 1 FROM [dbo].[Task_Note]      WHERE [note_id] = @note_id)
        SET @count = @count + 1;

    RETURN @count;
END;
