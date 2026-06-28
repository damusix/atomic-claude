-- Binary fact: Project_Tag
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Project_Tag" (
    tag_id              INT NOT NULL,
    project_id          INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_project_tag PRIMARY KEY (tag_id, project_id),
    CONSTRAINT fk_project_tag_tag_id FOREIGN KEY (tag_id) REFERENCES "Tag"(tag_id) ON DELETE CASCADE,
    CONSTRAINT fk_project_tag_project_id FOREIGN KEY (project_id) REFERENCES "Project"(project_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_tag_project_id ON "Project_Tag" (project_id);
