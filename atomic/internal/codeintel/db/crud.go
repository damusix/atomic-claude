package db

// CRUD prepared-statement layer for the code-intelligence DB.
//
// # Integer-bool convention
//
// SQLite stores is_exported/is_async/is_static/is_const as INTEGER (0/1).
// modernc.org/sqlite does not auto-convert INTEGER to Go bool, so every scan
// reads into a local int and converts: flag = n != 0. Writes go the other way:
// if b { return 1 } else { return 0 }.
//
// # JSON-in-TEXT convention
//
// Columns decorators/type_parameters/metadata/candidates/errors store opaque
// JSON blobs as TEXT. The types.Node/Edge/FileRecord structs use json.RawMessage
// for these fields. SQLite NULL round-trips as nil; a non-null TEXT byte-slice
// round-trips without mutation. The db layer uses *[]byte for scanning: a NULL
// column yields a nil pointer which maps to nil json.RawMessage; a non-null
// column yields a non-nil pointer whose value is the TEXT bytes.
//
// # Batch chunking (appendix O)
//
// Any variadic IN (...) query is split into chunks of at most
// SQLITE_PARAM_CHUNK_SIZE = 500 parameters. SQLite's SQLITE_LIMIT_VARIABLE_NUMBER
// defaults to 999 (or 32766 in newer builds) but the spec mandates 500 to match
// the reference implementation's explicit chunk size.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// jsonUnmarshal is an alias so the crud.go helpers can call json.Unmarshal
// without a bare "json" identifier in the call sites below.
var jsonUnmarshal = json.Unmarshal

// SQLITE_PARAM_CHUNK_SIZE is the maximum number of parameters per IN (...)
// clause (appendix O). Any variadic query must split its inputs into chunks of
// at most this size and union the results.
const SQLITE_PARAM_CHUNK_SIZE = 500

// ErrNotFound is returned by Get* methods when the requested row does not exist.
var ErrNotFound = errors.New("codeintel/db: not found")

// ---------------------------------------------------------------------------
// Node CRUD
// ---------------------------------------------------------------------------

// UpsertNode inserts or replaces a node row (INSERT OR REPLACE). The FTS5
// triggers (nodes_ai, nodes_au) keep nodes_fts in sync automatically.
// updated_at is stored as 0; use UpsertNodeAt to record a specific timestamp.
func (d *DB) UpsertNode(ctx context.Context, n types.Node) error {
	return d.UpsertNodeAt(ctx, n, 0)
}

// UpsertNodeAt inserts or replaces a node row with an explicit updatedAt Unix
// timestamp (seconds since epoch). The orchestrator (CP10) passes
// time.Now().Unix() so the re-index time is recorded per node.
func (d *DB) UpsertNodeAt(ctx context.Context, n types.Node, updatedAt int64) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO nodes
		  (id, kind, name, qualified_name, file_path, language,
		   start_line, end_line, start_column, end_column,
		   docstring, signature, visibility,
		   is_exported, is_async, is_static, is_const,
		   decorators, type_parameters, metadata, updated_at)
		VALUES
		  (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, string(n.Kind), n.Name, n.QualifiedName, n.FilePath, string(n.Language),
		n.StartLine, n.EndLine, n.StartColumn, n.EndColumn,
		n.Docstring, n.Signature, n.Visibility,
		boolToInt(n.IsExported), boolToInt(n.IsAsync), boolToInt(n.IsStatic), boolToInt(n.IsConst),
		rawOrNil(n.Decorators), rawOrNil(n.TypeParameters), rawOrNil(n.Metadata),
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: UpsertNodeAt %s: %w", n.ID, err)
	}
	return nil
}

// GetNode returns the node with the given id, or ErrNotFound if absent.
func (d *DB) GetNode(ctx context.Context, id string) (types.Node, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, kind, name, qualified_name, file_path, language,
		       start_line, end_line, start_column, end_column,
		       docstring, signature, visibility,
		       is_exported, is_async, is_static, is_const,
		       decorators, type_parameters, metadata, updated_at
		FROM nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return types.Node{}, fmt.Errorf("codeintel/db: GetNode %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return types.Node{}, fmt.Errorf("codeintel/db: GetNode %s: %w", id, err)
	}
	return n, nil
}

// GetNodesInFile returns all nodes with the given file_path.
func (d *DB) GetNodesInFile(ctx context.Context, filePath string) ([]types.Node, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, kind, name, qualified_name, file_path, language,
		       start_line, end_line, start_column, end_column,
		       docstring, signature, visibility,
		       is_exported, is_async, is_static, is_const,
		       decorators, type_parameters, metadata, updated_at
		FROM nodes WHERE file_path = ?`, filePath)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetNodesInFile %s: %w", filePath, err)
	}
	return collectNodes(rows)
}

// GetNodesByKind returns all nodes of the given kind.
func (d *DB) GetNodesByKind(ctx context.Context, kind types.NodeKind) ([]types.Node, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, kind, name, qualified_name, file_path, language,
		       start_line, end_line, start_column, end_column,
		       docstring, signature, visibility,
		       is_exported, is_async, is_static, is_const,
		       decorators, type_parameters, metadata, updated_at
		FROM nodes WHERE kind = ?`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetNodesByKind %s: %w", kind, err)
	}
	return collectNodes(rows)
}

// GetNodesByIds returns nodes for the given ids, chunking the IN (...) into
// batches of SQLITE_PARAM_CHUNK_SIZE to stay within SQLite's parameter limit.
// The returned slice may be in any order; callers that need deterministic order
// must sort the result.
func (d *DB) GetNodesByIds(ctx context.Context, ids []string) ([]types.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var result []types.Node
	for start := 0; start < len(ids); start += SQLITE_PARAM_CHUNK_SIZE {
		end := start + SQLITE_PARAM_CHUNK_SIZE
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		nodes, err := getNodesByIdsChunk(ctx, d.db, chunk)
		if err != nil {
			return nil, fmt.Errorf("codeintel/db: GetNodesByIds chunk %d-%d: %w", start, end, err)
		}
		result = append(result, nodes...)
	}
	return result, nil
}

// getNodesByIdsChunk executes a single IN (...) for a chunk of ≤500 ids.
func getNodesByIdsChunk(ctx context.Context, db *sql.DB, ids []string) ([]types.Node, error) {
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
	q := `SELECT id, kind, name, qualified_name, file_path, language,
		       start_line, end_line, start_column, end_column,
		       docstring, signature, visibility,
		       is_exported, is_async, is_static, is_const,
		       decorators, type_parameters, metadata, updated_at
		FROM nodes WHERE id IN (` + placeholders + `)`

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return collectNodes(rows)
}

// GetAllNodes returns all nodes in the database. Used by the resolution pipeline
// (CP13) warmCaches to build the known-names cache. On large repos this is a
// full table scan; it runs once per resolveAndPersistBatched invocation.
func (d *DB) GetAllNodes(ctx context.Context) ([]types.Node, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, kind, name, qualified_name, file_path, language,
		       start_line, end_line, start_column, end_column,
		       docstring, signature, visibility,
		       is_exported, is_async, is_static, is_const,
		       decorators, type_parameters, metadata, updated_at
		FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetAllNodes: %w", err)
	}
	return collectNodes(rows)
}

// DeleteNode deletes the node with the given id. FTS5 delete-sentinel trigger
// (nodes_ad) removes it from the FTS index automatically. FK CASCADE removes
// any edges that reference this node as source or target.
func (d *DB) DeleteNode(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteNode %s: %w", id, err)
	}
	return nil
}

// DeleteNodesByFile deletes all nodes whose file_path matches the given path.
// FK CASCADE (appendix A: edges.source/target ON DELETE CASCADE) removes every
// edge that references any of those nodes. FTS5 delete-sentinel triggers keep
// the FTS index consistent.
//
// This is the load-bearing sync primitive (R-E): because node-id embeds the
// line number, a moved symbol gets a new id. An in-place REPLACE would leave
// the old-id node orphaned with dangling edges. Deleting all of a file's nodes
// before re-extracting guarantees no orphans.
func (d *DB) DeleteNodesByFile(ctx context.Context, filePath string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM nodes WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteNodesByFile %s: %w", filePath, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Edge CRUD
// ---------------------------------------------------------------------------

// InsertEdge inserts a new edge (AUTOINCREMENT id) and returns the new row id.
// Use INSERT OR IGNORE semantics are not forced here — edge deduplication is the
// caller's responsibility. The returned id is the SQLite ROWID of the new edge.
func (d *DB) InsertEdge(ctx context.Context, e types.Edge) (int64, error) {
	res, err := d.db.ExecContext(ctx, `
		INSERT INTO edges (source, target, kind, metadata, line, col, provenance)
		VALUES (?,?,?,?,?,?,?)`,
		e.Source, e.Target, string(e.Kind),
		rawOrNil(e.Metadata), e.Line, e.Column, nullableString(e.Provenance),
	)
	if err != nil {
		return 0, fmt.Errorf("codeintel/db: InsertEdge %s→%s: %w", e.Source, e.Target, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("codeintel/db: InsertEdge LastInsertId: %w", err)
	}
	return id, nil
}

// GetEdgesBySource returns all edges with the given source node id.
func (d *DB) GetEdgesBySource(ctx context.Context, sourceID string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE source = ?`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesBySource %s: %w", sourceID, err)
	}
	return collectEdges(rows)
}

// GetEdgesByTarget returns all edges with the given target node id.
func (d *DB) GetEdgesByTarget(ctx context.Context, targetID string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE target = ?`, targetID)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesByTarget %s: %w", targetID, err)
	}
	return collectEdges(rows)
}

// GetAllEdges returns all edges in the database. Used by synthesizers that need
// to build a full target→edges map in one pass rather than querying per node.
func (d *DB) GetAllEdges(ctx context.Context) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetAllEdges: %w", err)
	}
	return collectEdges(rows)
}

// GetEdgesByProvenance returns all edges whose provenance equals the given
// string. Passing "heuristic" returns all synthesis-stamped edges in one query,
// avoiding the O(N-nodes) loop in loadExistingSynthEdges.
func (d *DB) GetEdgesByProvenance(ctx context.Context, provenance string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE provenance = ?`, provenance)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesByProvenance %q: %w", provenance, err)
	}
	return collectEdges(rows)
}

// DeleteEdge deletes the edge with the given id.
func (d *DB) DeleteEdge(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM edges WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteEdge %d: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// FileRecord CRUD
// ---------------------------------------------------------------------------

// UpsertFile inserts or replaces a file record (INSERT OR REPLACE by path PK).
func (d *DB) UpsertFile(ctx context.Context, f types.FileRecord) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO files
		  (path, content_hash, language, size, modified_at, indexed_at, node_count, errors)
		VALUES (?,?,?,?,?,?,?,?)`,
		f.Path, f.ContentHash, string(f.Language), f.Size,
		f.ModifiedAt, f.IndexedAt,
		f.NodeCount, rawOrNil(f.Errors),
	)
	if err != nil {
		return fmt.Errorf("codeintel/db: UpsertFile %s: %w", f.Path, err)
	}
	return nil
}

// GetFile returns the file record for the given path, or ErrNotFound if absent.
func (d *DB) GetFile(ctx context.Context, path string) (types.FileRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT path, content_hash, language, size, modified_at, indexed_at, node_count, errors
		FROM files WHERE path = ?`, path)
	f, err := scanFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return types.FileRecord{}, fmt.Errorf("codeintel/db: GetFile %s: %w", path, ErrNotFound)
	}
	if err != nil {
		return types.FileRecord{}, fmt.Errorf("codeintel/db: GetFile %s: %w", path, err)
	}
	return f, nil
}

// DeleteFile deletes the file record with the given path.
func (d *DB) DeleteFile(ctx context.Context, path string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteFile %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

// rowScanner is the common interface between *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanNode scans a single node row. The caller is responsible for checking
// sql.ErrNoRows before wrapping the error.
func scanNode(s rowScanner) (types.Node, error) {
	var (
		n          types.Node
		kind       string
		lang       string
		isExported int
		isAsync    int
		isStatic   int
		isConst    int
		decorators []byte
		typeParams []byte
		metadata   []byte
		updatedAt  int64
	)
	err := s.Scan(
		&n.ID, &kind, &n.Name, &n.QualifiedName, &n.FilePath, &lang,
		&n.StartLine, &n.EndLine, &n.StartColumn, &n.EndColumn,
		&n.Docstring, &n.Signature, &n.Visibility,
		&isExported, &isAsync, &isStatic, &isConst,
		&decorators, &typeParams, &metadata,
		&updatedAt,
	)
	if err != nil {
		return types.Node{}, err
	}
	n.Kind = types.NodeKind(kind)
	n.Language = types.Language(lang)
	n.IsExported = isExported != 0
	n.IsAsync = isAsync != 0
	n.IsStatic = isStatic != 0
	n.IsConst = isConst != 0
	n.Decorators = nullBytesToRaw(decorators)
	n.TypeParameters = nullBytesToRaw(typeParams)
	n.Metadata = nullBytesToRaw(metadata)
	// updated_at is stored as INTEGER; Node.UpdatedAt is string. Leave as ""
	// for zero; callers that need the timestamp can read it directly.
	if updatedAt != 0 {
		n.UpdatedAt = fmt.Sprintf("%d", updatedAt)
	}
	return n, nil
}

// collectNodes drains a *sql.Rows into a []types.Node and closes the rows.
func collectNodes(rows *sql.Rows) ([]types.Node, error) {
	defer rows.Close()
	var result []types.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, n)
	}
	return result, rows.Err()
}

// scanEdge scans a single edge row (id, source, target, kind, metadata, line, col, provenance).
func scanEdge(s rowScanner) (types.Edge, error) {
	var (
		e        types.Edge
		kind     string
		metadata []byte
	)
	err := s.Scan(
		&e.ID, &e.Source, &e.Target, &kind,
		&metadata, &e.Line, &e.Column, &e.Provenance,
	)
	if err != nil {
		return types.Edge{}, err
	}
	e.Kind = types.EdgeKind(kind)
	e.Metadata = nullBytesToRaw(metadata)
	return e, nil
}

// collectEdges drains a *sql.Rows into a []types.Edge and closes the rows.
func collectEdges(rows *sql.Rows) ([]types.Edge, error) {
	defer rows.Close()
	var result []types.Edge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// scanFile scans a single file row.
func scanFile(s rowScanner) (types.FileRecord, error) {
	var (
		f      types.FileRecord
		lang   string
		errors []byte
	)
	err := s.Scan(
		&f.Path, &f.ContentHash, &lang, &f.Size,
		&f.ModifiedAt, &f.IndexedAt, &f.NodeCount, &errors,
	)
	if err != nil {
		return types.FileRecord{}, err
	}
	f.Language = types.Language(lang)
	f.Errors = nullBytesToRaw(errors)
	return f, nil
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

// boolToInt converts a Go bool to the SQLite INTEGER convention (0/1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullBytesToRaw converts a possibly-nil []byte to json.RawMessage.
// A nil slice (SQL NULL) maps to nil RawMessage. A non-nil slice maps to the
// bytes as-is — no copy needed since json.RawMessage is []byte.
func nullBytesToRaw(b []byte) []byte {
	if b == nil {
		return nil
	}
	return b
}

// rawOrNil converts a json.RawMessage to the value to pass to ExecContext.
// A nil RawMessage maps to nil (which SQLite stores as NULL). A non-nil
// RawMessage maps to the bytes as a string argument.
func rawOrNil(r []byte) any {
	if r == nil {
		return nil
	}
	return string(r)
}

// nullableString converts an empty string to nil (stored as NULL) and a
// non-empty string to the value itself.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stringSliceToJSON encodes a []string as a JSON array for SQLite TEXT storage.
// A nil or empty slice maps to nil (stored as NULL), matching the candidates
// column convention. Only string-literal content is encoded — the resulting JSON
// has no surrounding whitespace.
func stringSliceToJSON(ss []string) any {
	if len(ss) == 0 {
		return nil
	}
	// Hand-encode to avoid an encoding/json import for a trivial case.
	// Each element is JSON-escaped; the result is a compact array.
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range ss {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

// jsonToStringSlice decodes a nullable JSON TEXT column into []string.
// A nil byte slice (SQL NULL) or empty byte slice maps to nil. A non-nil byte
// slice is JSON-decoded as []string; decode errors are ignored (returns nil)
// since the column value is always written by stringSliceToJSON.
func jsonToStringSlice(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	// Minimal hand-decode for a JSON string array produced by stringSliceToJSON.
	// We use encoding/json for correctness — the value is always a compact array.
	var result []string
	if err := jsonUnmarshal(b, &result); err != nil {
		return nil
	}
	return result
}
