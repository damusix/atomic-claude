-- Subtype: Task_StateTransition
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Task_StateTransition" (
    transition_id   INT NOT NULL,
    milestone_id    INT NOT NULL,
    task_no         INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task_state_transition PRIMARY KEY (transition_id),
    CONSTRAINT fk_task_state_transition_basetype FOREIGN KEY (transition_id) REFERENCES "StateTransition"(transition_id) ON DELETE CASCADE,
    CONSTRAINT fk_task_state_transition_task FOREIGN KEY (milestone_id, task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_state_transition_task ON "Task_StateTransition" (milestone_id, task_no);
