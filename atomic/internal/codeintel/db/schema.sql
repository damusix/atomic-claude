-- Code-intelligence engine schema (appendix A, verbatim).
-- All statements use IF NOT EXISTS for idempotent init.

-- ---------------------------------------------------------------------------
-- nodes: symbol nodes in the knowledge graph
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    name            TEXT NOT NULL,
    qualified_name  TEXT NOT NULL,
    file_path       TEXT NOT NULL,
    language        TEXT NOT NULL,
    start_line      INTEGER NOT NULL DEFAULT 0,
    end_line        INTEGER NOT NULL DEFAULT 0,
    start_column    INTEGER NOT NULL DEFAULT 0,
    end_column      INTEGER NOT NULL DEFAULT 0,
    docstring       TEXT,
    signature       TEXT,
    visibility      TEXT,
    is_exported     INTEGER NOT NULL DEFAULT 0,
    is_async        INTEGER NOT NULL DEFAULT 0,
    is_static       INTEGER NOT NULL DEFAULT 0,
    is_const        INTEGER NOT NULL DEFAULT 0,
    decorators      TEXT,
    type_parameters TEXT,
    metadata        TEXT,
    updated_at      INTEGER NOT NULL DEFAULT 0
);

-- ---------------------------------------------------------------------------
-- edges: directed relationships between nodes
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS edges (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    source      TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target      TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    metadata    TEXT,
    line        INTEGER NOT NULL DEFAULT 0,
    col         INTEGER NOT NULL DEFAULT 0,
    provenance  TEXT DEFAULT NULL
);

-- ---------------------------------------------------------------------------
-- files: indexed file records
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS files (
    path         TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL DEFAULT '',
    language     TEXT NOT NULL DEFAULT '',
    size         INTEGER NOT NULL DEFAULT 0,
    modified_at  INTEGER NOT NULL DEFAULT 0,
    indexed_at   INTEGER NOT NULL DEFAULT 0,
    node_count   INTEGER NOT NULL DEFAULT 0,
    errors       TEXT
);

-- ---------------------------------------------------------------------------
-- unresolved_refs: references awaiting resolution into edges
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS unresolved_refs (
    id             TEXT PRIMARY KEY,
    from_node_id   TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    reference_name TEXT NOT NULL,
    reference_kind TEXT NOT NULL,
    line           INTEGER NOT NULL DEFAULT 0,
    col            INTEGER NOT NULL DEFAULT 0,
    candidates     TEXT,
    file_path      TEXT NOT NULL DEFAULT '',
    language       TEXT NOT NULL DEFAULT 'unknown'
);

-- ---------------------------------------------------------------------------
-- project_metadata: key/value store for engine state
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS project_metadata (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0
);

-- ---------------------------------------------------------------------------
-- FTS5 virtual table for full-text search over nodes
-- External-content vtable: content='nodes', content_rowid='rowid'
-- Column order matches BM25 weight vector (0,20,5,1,2) from appendix J:
--   id(0), name(20), qualified_name(5), docstring(1), signature(2)
-- ---------------------------------------------------------------------------
CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
    id,
    name,
    qualified_name,
    docstring,
    signature,
    content='nodes',
    content_rowid='rowid'
);

-- ---------------------------------------------------------------------------
-- FTS5 sync triggers (external-content protocol)
-- nodes_ai: after INSERT — add new row to FTS index
-- nodes_ad: after DELETE — emit delete-sentinel to remove from FTS index
-- nodes_au: after UPDATE — delete-sentinel for old row, insert new row
-- The ('delete', …) sentinel form is mandatory for external-content FTS5 tables.
-- ---------------------------------------------------------------------------
CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO nodes_fts(rowid, id, name, qualified_name, docstring, signature)
    VALUES (NEW.rowid, NEW.id, NEW.name, NEW.qualified_name, NEW.docstring, NEW.signature);
END;

CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, id, name, qualified_name, docstring, signature)
    VALUES ('delete', OLD.rowid, OLD.id, OLD.name, OLD.qualified_name, OLD.docstring, OLD.signature);
END;

CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, id, name, qualified_name, docstring, signature)
    VALUES ('delete', OLD.rowid, OLD.id, OLD.name, OLD.qualified_name, OLD.docstring, OLD.signature);
    INSERT INTO nodes_fts(rowid, id, name, qualified_name, docstring, signature)
    VALUES (NEW.rowid, NEW.id, NEW.name, NEW.qualified_name, NEW.docstring, NEW.signature);
END;

-- ---------------------------------------------------------------------------
-- Indexes
-- Edge indexes: composites only — narrow idx_edges_source / idx_edges_target
-- are intentionally absent (appendix A: v4 dropped them; composites cover
-- source-only / target-only lookups via left-prefix).
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_edges_kind         ON edges(kind);
CREATE INDEX IF NOT EXISTS idx_edges_source_kind  ON edges(source, kind);
CREATE INDEX IF NOT EXISTS idx_edges_target_kind  ON edges(target, kind);
CREATE INDEX IF NOT EXISTS idx_edges_provenance   ON edges(provenance);

-- Node indexes
CREATE INDEX IF NOT EXISTS idx_nodes_lower_name   ON nodes(lower(name));
