-- Binary fact: Project_Memory
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Project_Memory" (
    project_id          INT NOT NULL,
    memory_id           INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_project_memory PRIMARY KEY (project_id, memory_id),
    CONSTRAINT fk_project_memory_project_id FOREIGN KEY (project_id) REFERENCES "Project"(project_id) ON DELETE CASCADE,
    CONSTRAINT fk_project_memory_memory_id FOREIGN KEY (memory_id) REFERENCES "Memory"(memory_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_memory_memory_id ON "Project_Memory" (memory_id);
