-- Subtype: Milestone_Note
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Milestone_Note" (
    note_id        INT NOT NULL,
    milestone_id   INT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_milestone_note PRIMARY KEY (note_id),
    CONSTRAINT fk_milestone_note_note FOREIGN KEY (note_id) REFERENCES "Note"(note_id) ON DELETE CASCADE,
    CONSTRAINT fk_milestone_note_milestone FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_milestone_note_milestone ON "Milestone_Note" (milestone_id);
