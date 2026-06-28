-- Binary fact: Task_Dependency
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Task_Dependency" (
    milestone_id        INT NOT NULL,
    task_no             INT NOT NULL,
    dep_milestone_id    INT NOT NULL,
    dep_task_no         INT NOT NULL,
    dependency_verb     VARCHAR(32) NOT NULL,
    reason              VARCHAR(255) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_task_dependency PRIMARY KEY (milestone_id, task_no, dep_milestone_id, dep_task_no),
    CONSTRAINT fk_task_dependency_task FOREIGN KEY (milestone_id, task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE,
    CONSTRAINT fk_task_dependency_dep_task FOREIGN KEY (dep_milestone_id, dep_task_no) REFERENCES "Task"(milestone_id, task_no) ON DELETE CASCADE,
    CONSTRAINT fk_task_dependency_dependency_verb FOREIGN KEY (dependency_verb) REFERENCES "DependencyVerb"(dependency_verb) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_task_dependency_dep_milestone_id_dep_task_no ON "Task_Dependency" (dep_milestone_id, dep_task_no);
CREATE INDEX IF NOT EXISTS idx_task_dependency_dependency_verb ON "Task_Dependency" (dependency_verb);
