-- Binary fact: Milestone_Tag
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Milestone_Tag" (
    tag_id              INT NOT NULL,
    milestone_id        INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_milestone_tag PRIMARY KEY (tag_id, milestone_id),
    CONSTRAINT fk_milestone_tag_tag_id FOREIGN KEY (tag_id) REFERENCES "Tag"(tag_id) ON DELETE CASCADE,
    CONSTRAINT fk_milestone_tag_milestone_id FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_milestone_tag_milestone_id ON "Milestone_Tag" (milestone_id);
