-- Hierarchic child: Task (child of Milestone)
-- Source: tmp/llm-memory-db.pseudo  (HIERARCHIC CHILDREN section)
--
-- Composite PK (milestone_id, task_no). task_no assigned MAX+1 per milestone_id at proc layer.
-- Cascade from Milestone: when a milestone is hard-deleted (cleanup), tasks go with it.

CREATE TABLE IF NOT EXISTS "Task" (
    milestone_id        INT NOT NULL,
    task_no             INT NOT NULL,
    tracking_status     VARCHAR(32) NOT NULL,
    agent_id            INT NOT NULL,
    title               VARCHAR(255) NOT NULL DEFAULT '',
    content             TEXT NOT NULL DEFAULT '',
    reason              VARCHAR(255) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task PRIMARY KEY (milestone_id, task_no),
    CONSTRAINT fk_task_milestone_id FOREIGN KEY (milestone_id) REFERENCES "Milestone"(milestone_id) ON DELETE CASCADE,
    CONSTRAINT fk_task_tracking_status FOREIGN KEY (tracking_status) REFERENCES "TrackingStatus"(tracking_status) ON DELETE RESTRICT,
    CONSTRAINT fk_task_agent_id FOREIGN KEY (agent_id) REFERENCES "Agent"(agent_id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_task_milestone_id ON "Task" (milestone_id);
CREATE INDEX IF NOT EXISTS idx_task_tracking_status ON "Task" (tracking_status);
CREATE INDEX IF NOT EXISTS idx_task_agent_id ON "Task" (agent_id);
