CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Attach_Project]
    @milestone_id INT,
    @project_id   INT
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1
        FROM [dbo].[Project_Milestone]
        WHERE [project_id]   = @project_id
          AND [milestone_id] = @milestone_id
    )
        INSERT INTO [dbo].[Project_Milestone] ([project_id], [milestone_id])
            VALUES (@project_id, @milestone_id);
END
