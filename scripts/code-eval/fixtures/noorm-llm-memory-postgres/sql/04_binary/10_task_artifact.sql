-- Binary fact: Task_Artifact
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Task_Artifact" (
    milestone_id        INT NOT NULL,
    task_no             INT NOT NULL,
    artifact_id         INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task_artifact PRIMARY KEY (milestone_id, task_no, artifact_id),
    CONSTRAINT fk_task_artifact_task FOREIGN KEY (milestone_id, task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE,
    CONSTRAINT fk_task_artifact_artifact_id FOREIGN KEY (artifact_id) REFERENCES "Artifact"(artifact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_artifact_artifact_id ON "Task_Artifact" (artifact_id);
