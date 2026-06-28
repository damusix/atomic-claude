-- Binary fact: Memory_Tag
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Memory_Tag" (
    tag_id              INT NOT NULL,
    memory_id           INT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_memory_tag PRIMARY KEY (tag_id, memory_id),
    CONSTRAINT fk_memory_tag_tag_id FOREIGN KEY (tag_id) REFERENCES "Tag"(tag_id) ON DELETE CASCADE,
    CONSTRAINT fk_memory_tag_memory_id FOREIGN KEY (memory_id) REFERENCES "Memory"(memory_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_tag_memory_id ON "Memory_Tag" (memory_id);
