-- =============================================================================
-- vw_Related_Memory
-- -----------------------------------------------------------------------------
-- Symmetric view over Related_Memory. The base table stores one direction per
-- row (with verb_forward); this view exposes both directions, substituting
-- verb_backward when inverting. Symmetric verbs ('contradicts', 'related-to',
-- 'equivalent-to') have verb_forward = verb_backward in MemoryRelationVerb.
--
-- Columns: memory_id, related_memory_id, verb, reason, created_at.
--
-- Query: WHERE memory_id = ? returns all relations from that memory's
-- perspective with the verb correctly oriented.
-- =============================================================================
CREATE OR ALTER VIEW [dbo].[vw_Related_Memory]
AS
SELECT
    rm.[memory_id],
    rm.[related_memory_id],
    mrv.[verb_forward] AS [verb],
    rm.[reason],
    rm.[created_at]
FROM [dbo].[Related_Memory] rm
INNER JOIN [dbo].[MemoryRelationVerb] mrv
    ON mrv.[verb_forward] = rm.[relation_verb]
UNION ALL
SELECT
    rm.[related_memory_id] AS [memory_id],
    rm.[memory_id]         AS [related_memory_id],
    mrv.[verb_backward]    AS [verb],
    rm.[reason],
    rm.[created_at]
FROM [dbo].[Related_Memory] rm
INNER JOIN [dbo].[MemoryRelationVerb] mrv
    ON mrv.[verb_forward] = rm.[relation_verb];
