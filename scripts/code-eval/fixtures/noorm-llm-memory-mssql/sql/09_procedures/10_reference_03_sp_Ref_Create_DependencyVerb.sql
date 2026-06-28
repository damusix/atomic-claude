CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Create_DependencyVerb]
    @dependency_verb VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF NOT EXISTS (
        SELECT 1 FROM [dbo].[DependencyVerb] WHERE [dependency_verb] = @dependency_verb
    )
    BEGIN
        INSERT INTO [dbo].[DependencyVerb] ([dependency_verb]) VALUES (@dependency_verb);
    END
END
