-- Reference: MemoryDomain
-- Source: tmp/llm-memory-db.pseudo  (REFERENCE TABLES section)

CREATE TABLE IF NOT EXISTS "MemoryDomain" (
    domain              VARCHAR(32) NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (domain)
);
