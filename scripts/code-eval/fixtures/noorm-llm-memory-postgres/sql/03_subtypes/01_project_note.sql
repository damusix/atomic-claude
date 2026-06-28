-- Subtype: Project_Note
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES section)
-- Exclusivity enforced by triggers in sql/09_triggers/.

CREATE TABLE IF NOT EXISTS "Project_Note" (
    note_id        INT NOT NULL,
    project_id     INT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_project_note PRIMARY KEY (note_id),
    CONSTRAINT fk_project_note_note FOREIGN KEY (note_id) REFERENCES "Note"(note_id) ON DELETE CASCADE,
    CONSTRAINT fk_project_note_project FOREIGN KEY (project_id) REFERENCES "Project"(project_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_note_project ON "Project_Note" (project_id);
