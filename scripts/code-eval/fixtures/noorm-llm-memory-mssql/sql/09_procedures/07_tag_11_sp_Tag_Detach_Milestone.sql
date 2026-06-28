CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Detach_Milestone]
    @tag_id       INT,
    @milestone_id INT
AS
BEGIN
    SET NOCOUNT ON;

    DELETE FROM [dbo].[Milestone_Tag]
        WHERE [tag_id]       = @tag_id
          AND [milestone_id] = @milestone_id;
END
