CREATE OR ALTER PROCEDURE [dbo].[sp_Artifact_Attach_Milestone]
    @artifact_id  INT,
    @milestone_id INT
AS
BEGIN
    SET NOCOUNT ON;

    -- Idempotent: skip if already attached
    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[Milestone_Artifact]
        WHERE [milestone_id] = @milestone_id AND [artifact_id] = @artifact_id
    )
    BEGIN
        INSERT INTO [dbo].[Milestone_Artifact] ([milestone_id], [artifact_id])
            VALUES (@milestone_id, @artifact_id);
    END
END
