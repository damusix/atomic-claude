-- =============================================================================
-- fn_NoteIsOfType (note_id, expected_type) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 if Note(@note_id) has note_type = @expected_type, else 0.
-- Used by inline CHECK constraints on the *_Note subtype tables to enforce
-- the discriminator without triggers. Returns 0 for non-existent note_ids
-- (FK on the subtype row catches the missing parent separately).
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_NoteIsOfType](
    @note_id       INT,
    @expected_type VARCHAR(32)
)
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @match BIT = 0;

    SELECT @match = CASE WHEN [note_type] = @expected_type THEN 1 ELSE 0 END
    FROM [dbo].[Note]
    WHERE [note_id] = @note_id;

    RETURN COALESCE(@match, 0);
END;
