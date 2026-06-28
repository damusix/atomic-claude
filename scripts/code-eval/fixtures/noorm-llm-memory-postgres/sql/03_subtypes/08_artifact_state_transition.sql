-- Subtype: Artifact_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Artifact_StateTransition" (
    transition_id   INT NOT NULL,
    artifact_id     INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_artifact_state_transition PRIMARY KEY (transition_id),
    CONSTRAINT fk_artifact_state_transition_basetype FOREIGN KEY (transition_id) REFERENCES "StateTransition"(transition_id) ON DELETE CASCADE,
    CONSTRAINT fk_artifact_state_transition_artifact FOREIGN KEY (artifact_id) REFERENCES "Artifact"(artifact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_artifact_state_transition_artifact ON "Artifact_StateTransition" (artifact_id);
