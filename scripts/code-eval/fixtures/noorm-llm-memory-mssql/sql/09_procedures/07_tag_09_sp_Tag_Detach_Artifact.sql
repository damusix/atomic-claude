CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Detach_Artifact]
    @tag_id      INT,
    @artifact_id INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Artifact_Tag]
        WHERE [tag_id]      = @tag_id
          AND [artifact_id] = @artifact_id;
END
