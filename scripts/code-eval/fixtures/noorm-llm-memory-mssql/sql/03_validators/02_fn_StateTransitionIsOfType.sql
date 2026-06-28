-- =============================================================================
-- fn_StateTransitionIsOfType (transition_id, expected_type) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 if StateTransition(@transition_id) has the given discriminator
-- value in [state_transition_type], else 0. Used by inline CHECK constraints
-- on the *_StateTransition subtype tables (single-axis subtypes:
-- Memory_StateTransition, Note_StateTransition, Artifact_StateTransition,
-- Task_StateTransition). Milestone_StateTransition uses the two-axis
-- companion fn_StateTransitionIsMilestoneAxis instead.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_StateTransitionIsOfType](
    @transition_id INT,
    @expected_type VARCHAR(32)
)
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @match BIT = 0;

    SELECT @match = CASE WHEN [state_transition_type] = @expected_type THEN 1 ELSE 0 END
    FROM [dbo].[StateTransition]
    WHERE [transition_id] = @transition_id;

    RETURN COALESCE(@match, 0);
END;
