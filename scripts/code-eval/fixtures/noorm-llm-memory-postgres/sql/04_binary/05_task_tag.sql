-- Binary fact: Task_Tag
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Task_Tag" (
    tag_id              INT NOT NULL,
    milestone_id        INT NOT NULL,
    task_no             INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task_tag PRIMARY KEY (tag_id, milestone_id, task_no),
    CONSTRAINT fk_task_tag_tag_id FOREIGN KEY (tag_id) REFERENCES "Tag"(tag_id) ON DELETE CASCADE,
    CONSTRAINT fk_task_tag_task FOREIGN KEY (milestone_id, task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_tag_milestone_id_task_no ON "Task_Tag" (milestone_id, task_no);
