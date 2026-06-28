-- Reference: NoteType
-- Source: tmp/llm-memory-db.pseudo  (REFERENCE TABLES section)

CREATE TABLE IF NOT EXISTS "NoteType" (
    note_type           VARCHAR(32) NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (note_type)
);
