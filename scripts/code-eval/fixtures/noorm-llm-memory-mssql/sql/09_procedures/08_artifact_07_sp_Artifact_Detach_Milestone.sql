CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Detach_Milestone]
    @artifact_id  INT,
    @milestone_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Silent on missing row: detach is idempotent.
    DELETE FROM [dbo].[Milestone_Artifact]
        WHERE [milestone_id] = @milestone_id
          AND [artifact_id]  = @artifact_id;
END
