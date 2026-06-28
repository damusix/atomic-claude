-- Subtype: Memory_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Memory_StateTransition" (
    transition_id   INT NOT NULL,
    memory_id       INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_memory_state_transition PRIMARY KEY (transition_id),
    CONSTRAINT fk_memory_state_transition_basetype FOREIGN KEY (transition_id) REFERENCES "StateTransition"(transition_id) ON DELETE CASCADE,
    CONSTRAINT fk_memory_state_transition_memory FOREIGN KEY (memory_id) REFERENCES "Memory"(memory_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_state_transition_memory ON "Memory_StateTransition" (memory_id);
