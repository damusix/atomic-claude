CREATE OR ALTER PROCEDURE [dbo].[sp_Milestone_Detach_Project]
    @milestone_id INT,
    @project_id   INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Project_Milestone]
        WHERE [project_id]   = @project_id
          AND [milestone_id] = @milestone_id;
END
