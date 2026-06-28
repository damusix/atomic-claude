-- Binary fact: Artifact_Tag
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Artifact_Tag" (
    tag_id              INT NOT NULL,
    artifact_id         INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_artifact_tag PRIMARY KEY (tag_id, artifact_id),
    CONSTRAINT fk_artifact_tag_tag_id FOREIGN KEY (tag_id) REFERENCES "Tag"(tag_id) ON DELETE CASCADE,
    CONSTRAINT fk_artifact_tag_artifact_id FOREIGN KEY (artifact_id) REFERENCES "Artifact"(artifact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_artifact_tag_artifact_id ON "Artifact_Tag" (artifact_id);
