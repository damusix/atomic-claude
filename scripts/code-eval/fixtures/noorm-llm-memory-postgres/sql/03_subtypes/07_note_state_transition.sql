-- Subtype: Note_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Note_StateTransition" (
    transition_id   INT NOT NULL,
    note_id         INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_note_state_transition PRIMARY KEY (transition_id),
    CONSTRAINT fk_note_state_transition_basetype FOREIGN KEY (transition_id) REFERENCES "StateTransition"(transition_id) ON DELETE CASCADE,
    CONSTRAINT fk_note_state_transition_note FOREIGN KEY (note_id) REFERENCES "Note"(note_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_note_state_transition_note ON "Note_StateTransition" (note_id);
