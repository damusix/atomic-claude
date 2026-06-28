-- View: vw_Related_Memory
-- Source: tmp/llm-memory-db.pseudo  (VIEWS section)
--
-- Symmetric view over Related_Memory. Storage holds one direction per row with
-- a forward verb; this view exposes both directions, swapping in the backward
-- verb on the inverted branch. Symmetric verbs (contradicts / equivalent-to /
-- related-to) read the same value on both sides — that is intentional.

DROP VIEW IF EXISTS "vw_Related_Memory" CASCADE;

CREATE VIEW "vw_Related_Memory" AS
    SELECT
        rm.memory_id          AS memory_id,
        rm.related_memory_id  AS related_memory_id,
        mrv.verb_forward      AS verb,
        rm.reason,
        rm.created_at
    FROM "Related_Memory" rm
    INNER JOIN "MemoryRelationVerb" mrv ON mrv.verb_forward = rm.relation_verb

    UNION ALL

    SELECT
        rm.related_memory_id  AS memory_id,
        rm.memory_id          AS related_memory_id,
        mrv.verb_backward     AS verb,
        rm.reason,
        rm.created_at
    FROM "Related_Memory" rm
    INNER JOIN "MemoryRelationVerb" mrv ON mrv.verb_forward = rm.relation_verb;
