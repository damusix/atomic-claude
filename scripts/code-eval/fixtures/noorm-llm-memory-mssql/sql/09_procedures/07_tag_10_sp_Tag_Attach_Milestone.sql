-- ---------------------------------------------------------------------------
-- Attach / Detach — Milestone
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Attach_Milestone]
    @tag_id       INT,
    @milestone_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Milestone_Tag]
        WHERE [tag_id]       = @tag_id
          AND [milestone_id] = @milestone_id
    )
        INSERT INTO [dbo].[Milestone_Tag] ([tag_id], [milestone_id])
            VALUES (@tag_id, @milestone_id);
END
