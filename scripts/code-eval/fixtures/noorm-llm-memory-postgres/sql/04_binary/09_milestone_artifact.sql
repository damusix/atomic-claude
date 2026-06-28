-- Binary fact: Milestone_Artifact
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Milestone_Artifact" (
    milestone_id        INT NOT NULL,
    artifact_id         INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_milestone_artifact PRIMARY KEY (milestone_id, artifact_id),
    CONSTRAINT fk_milestone_artifact_milestone_id FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE,
    CONSTRAINT fk_milestone_artifact_artifact_id FOREIGN KEY (artifact_id) REFERENCES "Artifact"(artifact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_milestone_artifact_artifact_id ON "Milestone_Artifact" (artifact_id);
