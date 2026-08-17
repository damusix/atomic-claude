package db

// CRUD layer for the code-intelligence DB. Three conventions run through it:
//
// Integer-bool: modernc.org/sqlite will not convert INTEGER to Go bool, so
// every bool column is scanned into an int and converted by hand.
//
// JSON-in-TEXT: decorators, type_parameters, metadata, candidates, and errors
// are opaque JSON blobs. SQL NULL round-trips as a nil json.RawMessage; a
// non-null column's bytes pass through unmutated.
//
// Batch chunking: every variadic IN (...) splits at SQLITE_PARAM_CHUNK_SIZE.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

var jsonUnmarshal = json.Unmarshal

// SQLITE_PARAM_CHUNK_SIZE caps parameters per IN (...) clause. Well under
// SQLite's own limit, and fixed so chunk boundaries stay reproducible.
const SQLITE_PARAM_CHUNK_SIZE = 500

// ErrNotFound is returned by Get* methods when the requested row does not exist.
var ErrNotFound = errors.New("codeintel/db: not found")

// UpsertNode stores updated_at as 0; use UpsertNodeAt to record a timestamp.
func (d *DB) UpsertNode(ctx context.Context, n types.Node) error {
	return d.UpsertNodeAt(ctx, n, 0)
}

// UpsertNodeAt inserts or replaces a node, stamping updatedAt (Unix seconds)
// so the re-index time is recorded per node. FTS5 triggers keep nodes_fts in
// sync.
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

// GetNode returns ErrNotFound when id has no row.
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

// GetNodesInFile returns every node declared in filePath.
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

// GetNodesByIds chunks the IN (...) to stay inside SQLite's parameter limit.
// Results come back in arbitrary order.
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

// GetAllNodes is a full table scan, run once per batched resolution pass to
// warm the known-names cache.
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

// DeleteNode also drops the node's FTS row and, by FK cascade, every edge
// referencing it as source or target.
func (d *DB) DeleteNode(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteNode %s: %w", id, err)
	}
	return nil
}

// DeleteNodesByFile is the load-bearing sync primitive: a node id embeds its
// line, so a moved symbol gets a new id and an in-place REPLACE would strand
// the old node with dangling edges. Clearing the file first guarantees no
// orphans; FK cascade takes the edges with it.
func (d *DB) DeleteNodesByFile(ctx context.Context, filePath string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM nodes WHERE file_path = ?", filePath)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteNodesByFile %s: %w", filePath, err)
	}
	return nil
}

// InsertEdge returns the new ROWID. It does not dedup — that is the caller's
// job.
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

// GetEdgesBySource returns the outbound edges of sourceID.
func (d *DB) GetEdgesBySource(ctx context.Context, sourceID string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE source = ?`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesBySource %s: %w", sourceID, err)
	}
	return collectEdges(rows)
}

// GetEdgesByTarget returns the inbound edges of targetID.
func (d *DB) GetEdgesByTarget(ctx context.Context, targetID string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE target = ?`, targetID)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesByTarget %s: %w", targetID, err)
	}
	return collectEdges(rows)
}

// GetAllEdges is a full table scan, for synthesizers building a target→edges
// map in one pass instead of querying per node.
func (d *DB) GetAllEdges(ctx context.Context) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetAllEdges: %w", err)
	}
	return collectEdges(rows)
}

// GetEdgesByProvenance fetches a whole provenance class in one query, sparing
// loadExistingSynthEdges an O(nodes) loop.
func (d *DB) GetEdgesByProvenance(ctx context.Context, provenance string) ([]types.Edge, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, source, target, kind, metadata, line, col, COALESCE(provenance,'')
		FROM edges WHERE provenance = ?`, provenance)
	if err != nil {
		return nil, fmt.Errorf("codeintel/db: GetEdgesByProvenance %q: %w", provenance, err)
	}
	return collectEdges(rows)
}

// DeleteEdge removes one edge by ROWID.
func (d *DB) DeleteEdge(ctx context.Context, id int64) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM edges WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteEdge %d: %w", id, err)
	}
	return nil
}

// UpsertFile replaces by path, the primary key.
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

// GetFile returns ErrNotFound when path has no row.
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

// DeleteFile removes only the file row; nodes are DeleteNodesByFile's job.
func (d *DB) DeleteFile(ctx context.Context, path string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM files WHERE path = ?", path)
	if err != nil {
		return fmt.Errorf("codeintel/db: DeleteFile %s: %w", path, err)
	}
	return nil
}

// rowScanner is the common interface between *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanNode leaves sql.ErrNoRows unwrapped for the caller to classify.
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
	// Column is INTEGER, Node.UpdatedAt is string; zero stays "".
	if updatedAt != 0 {
		n.UpdatedAt = fmt.Sprintf("%d", updatedAt)
	}
	return n, nil
}

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

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullBytesToRaw needs no copy: json.RawMessage is already []byte.
func nullBytesToRaw(b []byte) []byte {
	if b == nil {
		return nil
	}
	return b
}

// rawOrNil turns a JSON blob into an ExecContext argument, nil becoming NULL.
func rawOrNil(r []byte) any {
	if r == nil {
		return nil
	}
	return string(r)
}

// nullIfEmpty keeps optional TEXT columns NULL rather than empty-string.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// stringSliceToJSON encodes a compact JSON array for TEXT storage; an empty
// slice becomes NULL, matching the candidates column convention.
func stringSliceToJSON(ss []string) any {
	if len(ss) == 0 {
		return nil
	}
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

// jsonToStringSlice inverts stringSliceToJSON. Decode errors return nil: the
// column is only ever written by that function.
func jsonToStringSlice(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	var result []string
	if err := jsonUnmarshal(b, &result); err != nil {
		return nil
	}
	return result
}
