-- Subtype: Milestone_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Milestone_StateTransition" (
    transition_id   INT NOT NULL,
    milestone_id    INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_milestone_state_transition PRIMARY KEY (transition_id),
    CONSTRAINT fk_milestone_state_transition_basetype FOREIGN KEY (transition_id) REFERENCES "StateTransition"(transition_id) ON DELETE CASCADE,
    CONSTRAINT fk_milestone_state_transition_milestone FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_milestone_state_transition_milestone ON "Milestone_StateTransition" (milestone_id);
