-- Binary fact: Project_Milestone
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Project_Milestone" (
    project_id          INT NOT NULL,
    milestone_id        INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_project_milestone PRIMARY KEY (project_id, milestone_id),
    CONSTRAINT fk_project_milestone_project_id FOREIGN KEY (project_id) REFERENCES "Project"(project_id) ON DELETE CASCADE,
    CONSTRAINT fk_project_milestone_milestone_id FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_milestone_milestone_id ON "Project_Milestone" (milestone_id);
