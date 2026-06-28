-- ---------------------------------------------------------------------------
-- Attach / Detach — Artifact
-- ---------------------------------------------------------------------------

CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Attach_Artifact]
    @tag_id      INT,
    @artifact_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Artifact_Tag]
        WHERE [tag_id]      = @tag_id
          AND [artifact_id] = @artifact_id
    )
        INSERT INTO [dbo].[Artifact_Tag] ([tag_id], [artifact_id])
            VALUES (@tag_id, @artifact_id);
END
