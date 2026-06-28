CREATE OR ALTER PROCEDURE [dbo].[sp_Ref_Delete_DependencyVerb]
    @dependency_verb VARCHAR(32)
AS
BEGIN
    SET NOCOUNT ON;

    IF EXISTS (
        SELECT 1 FROM [dbo].[Task_Dependency] WHERE [dependency_verb] = @dependency_verb
    )
    BEGIN
        RAISERROR('Cannot delete DependencyVerb %s — it is in use by one or more task dependencies.',
                  16, 1, @dependency_verb);
        RETURN;
    END

    DELETE FROM [dbo].[DependencyVerb] WHERE [dependency_verb] = @dependency_verb;
END
