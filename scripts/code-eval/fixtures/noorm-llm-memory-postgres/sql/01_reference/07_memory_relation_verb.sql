-- Reference: MemoryRelationVerb
-- Source: tmp/llm-memory-db.pseudo  (REFERENCE TABLES section)

CREATE TABLE IF NOT EXISTS "MemoryRelationVerb" (
    verb_forward        VARCHAR(32) NOT NULL,
    verb_backward       VARCHAR(32) NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (verb_forward)
);
