-- Binary fact: Related_Memory
-- Source: tmp/llm-memory-db.pseudo  (BINARY FACTS section)

CREATE TABLE IF NOT EXISTS "Related_Memory" (
    memory_id           INT NOT NULL,
    related_memory_id   INT NOT NULL,
    relation_verb       VARCHAR(32) NOT NULL,
    reason              VARCHAR(255) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_related_memory PRIMARY KEY (memory_id, related_memory_id),
    CONSTRAINT fk_related_memory_memory_id FOREIGN KEY (memory_id) REFERENCES "Memory"(memory_id) ON DELETE CASCADE,
    CONSTRAINT fk_related_memory_related_memory_id FOREIGN KEY (related_memory_id) REFERENCES "Memory"(memory_id) ON DELETE CASCADE,
    CONSTRAINT fk_related_memory_relation_verb FOREIGN KEY (relation_verb) REFERENCES "MemoryRelationVerb"(verb_forward) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_related_memory_related_memory_id ON "Related_Memory" (related_memory_id);
CREATE INDEX IF NOT EXISTS idx_related_memory_relation_verb ON "Related_Memory" (relation_verb);
