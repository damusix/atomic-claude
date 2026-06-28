-- =============================================================================
-- fn_StateTransitionIsMilestoneAxis (transition_id) -> BIT
-- -----------------------------------------------------------------------------
-- Returns 1 if StateTransition(@transition_id) has either of the two
-- milestone-axis discriminators ('milestone-tracking' or 'milestone-relevance'),
-- else 0. Milestone_StateTransition is the only subtype whose CHECK accepts
-- two state_transition_type values; this companion validator keeps that
-- CHECK a single readable expression.
-- =============================================================================
CREATE OR ALTER FUNCTION [dbo].[fn_StateTransitionIsMilestoneAxis](
    @transition_id INT
)
RETURNS BIT
WITH SCHEMABINDING
AS
BEGIN
    DECLARE @match BIT = 0;

    SELECT @match = CASE
        WHEN [state_transition_type] IN ('milestone-tracking', 'milestone-relevance')
        THEN 1 ELSE 0
    END
    FROM [dbo].[StateTransition]
    WHERE [transition_id] = @transition_id;

    RETURN COALESCE(@match, 0);
END;
