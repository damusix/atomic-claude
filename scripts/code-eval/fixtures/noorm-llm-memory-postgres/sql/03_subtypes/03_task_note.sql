-- Subtype: Task_Note
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Task_Note" (
    note_id        INT NOT NULL,
    milestone_id   INT NOT NULL,
    task_no        INT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task_note PRIMARY KEY (note_id),
    CONSTRAINT fk_task_note_note FOREIGN KEY (note_id) REFERENCES "Note"(note_id) ON DELETE CASCADE,
    CONSTRAINT fk_task_note_task FOREIGN KEY (milestone_id, task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_note_task ON "Task_Note" (milestone_id, task_no);
