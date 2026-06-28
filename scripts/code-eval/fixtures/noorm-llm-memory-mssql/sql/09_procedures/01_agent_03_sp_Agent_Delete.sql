CREATE OR ALTER PROCEDURE [dbo].[sp_Agent_Delete]
    @agent_id INT
AS
BEGIN
    SET NOCOUNT ON;

    IF @agent_id = 0
    BEGIN
        RAISERROR('Agent(0) is the sentinel and cannot be modified or deleted.', 16, 1);
        RETURN;
    END

    -- Reassign all entities pointing to this agent to sentinel 0
    UPDATE [dbo].[Project]         SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Note]            SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Tag]             SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Memory]          SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Artifact]        SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Milestone]       SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[Task]            SET [agent_id] = 0 WHERE [agent_id] = @agent_id;
    UPDATE [dbo].[StateTransition] SET [agent_id] = 0 WHERE [agent_id] = @agent_id;

    DELETE FROM [dbo].[Agent] WHERE [agent_id] = @agent_id;
END
