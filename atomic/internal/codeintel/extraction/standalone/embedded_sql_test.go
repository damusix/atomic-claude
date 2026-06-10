package standalone_test

// Tests for the embedded SQL admission gate and entry point (CP1).
//
// WHY these tests exist: the gate must admit real DDL/DML and reject prose,
// comments, and interpolated-table-target literals (post-substitution).
// The entry point must emit nodes with Provenance:"embedded" on all edges
// and produce file-absolute line numbers via newline padding.
//
// Substitution contract for harvesters (CP3/CP4): a harvester feeding this
// entry point is responsible for replacing language-specific interpolation
// segments with placeholder tokens before calling ExtractEmbeddedSQL.
// Specifically:
//   - An interpolated TABLE TARGET (e.g. Python f"SELECT a FROM {table}") must
//     be replaced so the resulting literal has no recognizable table name after
//     FROM/JOIN — e.g. replace "{table}" with "" or a keyword so the table-ref
//     regex produces no match.  The gate may still pass (SELECT + placeholder
//     elsewhere), but scanBodyEdges emits no table ref.
//   - An interpolated VALUE (e.g. f"... WHERE id = {id}") can be replaced with
//     a SQL placeholder like "?" or "$1"; the gate passes and the table ref
//     (literal "users") is extracted normally.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Gate tests
// ---------------------------------------------------------------------------

func TestIsSQLLiteral_CanonicalCorpus(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSQL bool
	}{
		{
			name:    "real DDL passes",
			input:   "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)",
			wantSQL: true,
		},
		{
			name:    "real DML passes",
			input:   "SELECT id, email FROM users WHERE active = $1",
			wantSQL: true,
		},
		{
			name:    "UI prose fails",
			input:   "choose an item from the dropdown",
			wantSQL: false,
		},
		{
			name:    "code comment prose fails",
			input:   "Copied from the original repo",
			wantSQL: false,
		},
		{
			// Interpolated table target: after harvester substitutes "{table}" with
			// an empty string, the literal becomes "SELECT a FROM  WHERE id = %s".
			// Gate may pass (SELECT + placeholder), but this tests the gate-pass
			// case in isolation — ExtractEmbeddedSQL is what produces zero refs.
			name:    "interpolated table target post-substitution - gate passes (FROM present, placeholder present)",
			input:   "SELECT a FROM  WHERE id = %s",
			wantSQL: true,
		},
		{
			// Interpolated value, literal table: harvester replaces "{id}" with "?"
			// yielding "SELECT a FROM users WHERE id = ?". Gate passes.
			name:    "interpolated value literal table post-substitution passes",
			input:   "SELECT a FROM users WHERE id = ?",
			wantSQL: true,
		},
		// Additional edge cases
		{
			name:    "CREATE VIEW passes",
			input:   "CREATE VIEW active_users AS SELECT id FROM users",
			wantSQL: true,
		},
		{
			name:    "INSERT INTO passes",
			input:   "INSERT INTO orders (user_id, total) VALUES ($1, $2)",
			wantSQL: true,
		},
		{
			name:    "UPDATE passes",
			input:   "UPDATE users SET email = $1 WHERE id = $2",
			wantSQL: true,
		},
		{
			name:    "DELETE FROM passes",
			input:   "DELETE FROM sessions WHERE expires_at < $1",
			wantSQL: true,
		},
		{
			name:    "DML without confidence discriminator fails",
			input:   "SELECT something",
			wantSQL: false,
		},
		{
			name:    "DML with comma (column list) passes",
			input:   "SELECT id, name FROM users",
			wantSQL: true,
		},
		{
			name:    "DML with comparison passes",
			input:   "SELECT id FROM users WHERE id > 0",
			wantSQL: true,
		},
		{
			name:    "DML with quoted literal passes",
			input:   "SELECT id FROM users WHERE status = 'active'",
			wantSQL: true,
		},
		{
			name:    "prose with FROM-like word fails",
			input:   "results from the database",
			wantSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := standalone.IsSQLLiteral(tt.input)
			if got != tt.wantSQL {
				t.Errorf("IsSQLLiteral(%q) = %v, want %v", tt.input, got, tt.wantSQL)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// UPDATE verb false-positive gate tests (CP2 DML gate tightening)
// ---------------------------------------------------------------------------

// TestIsSQLLiteral_UpdateSETGate verifies that UPDATE-verb strings only admit
// when a SET token is present. Prose like "UPDATE available: version %s" has
// the UPDATE verb and a confidence discriminator (%s) but never SET — without
// the extra guard those strings were falsely admitted.
func TestIsSQLLiteral_UpdateSETGate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSQL bool
	}{
		{
			// False-positive reproduction: "UPDATE available: version %s" has
			// UPDATE + %s (confidence) but no SET. Must be rejected.
			name:    "UPDATE prose with placeholder but no SET rejects",
			input:   "UPDATE available: version %s",
			wantSQL: false,
		},
		{
			// False-positive reproduction: "UPDATE plan len = %d" has
			// UPDATE + = (comparison confidence) but no SET. Must be rejected.
			// %d is not in dmlConfidenceRE but = is — this was the actual FP.
			name:    "UPDATE prose with comparison but no SET rejects",
			input:   "UPDATE plan len = %d",
			wantSQL: false,
		},
		{
			// Real SQL UPDATE with SET: must still admit.
			name:    "real UPDATE with SET admits",
			input:   "UPDATE users SET name = $1 WHERE id = $2",
			wantSQL: true,
		},
		// Regression: other DML verbs unaffected by the UPDATE/SET guard.
		{
			name:    "SELECT regression",
			input:   "SELECT id, email FROM users WHERE active = $1",
			wantSQL: true,
		},
		{
			name:    "INSERT INTO regression",
			input:   "INSERT INTO orders (user_id, total) VALUES ($1, $2)",
			wantSQL: true,
		},
		{
			name:    "DELETE FROM regression",
			input:   "DELETE FROM sessions WHERE expires_at < $1",
			wantSQL: true,
		},
		{
			name:    "MERGE INTO regression",
			input:   "MERGE INTO target t USING source s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.val = s.val",
			wantSQL: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := standalone.IsSQLLiteral(tt.input)
			if got != tt.wantSQL {
				t.Errorf("IsSQLLiteral(%q) = %v, want %v", tt.input, got, tt.wantSQL)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractEmbeddedSQL tests
// ---------------------------------------------------------------------------

func TestExtractEmbeddedSQL_DDLEmitsTableNode(t *testing.T) {
	// Real DDL path: CREATE TABLE → table node emitted.
	// WHY: DDL path must reuse SQLExtractor.Extract verbatim and produce a
	// table node with the correct name.
	const literal = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	const ownerID = "fn:owner:1"

	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/migrate.go", literal, 1, ownerID)

	tableNode := findSQLNode(result.Nodes, types.NodeKindTable, "users")
	if tableNode == nil {
		t.Fatalf("expected table node 'users', nodes = %v", nodeNames(result.Nodes))
	}
}

func TestExtractEmbeddedSQL_DDLEdgesHaveEmbeddedProvenance(t *testing.T) {
	// WHY: Every directly-created Edge from the embedded path must carry
	// Provenance:"embedded" so they are queryable via GetEdgesByProvenance.
	const literal = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/migrate.go", literal, 1, "fn:owner:1")

	for _, e := range result.Edges {
		if e.Provenance != "embedded" {
			t.Errorf("edge %s→%s has Provenance=%q, want %q", e.Source, e.Target, e.Provenance, "embedded")
		}
	}
}

func TestExtractEmbeddedSQL_DMLEmitsUnresolvedRef(t *testing.T) {
	// Real DML path: SELECT … FROM users → UnresolvedReference to users,
	// owned by the passed ownerNodeID.
	// WHY: DML path must call scanBodyEdges directly; owner = ownerNodeID.
	const literal = "SELECT id, email FROM users WHERE active = $1"
	const ownerID = "fn:owner:42"

	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/query.go", literal, 1, ownerID)

	if len(result.UnresolvedReferences) == 0 {
		t.Fatalf("expected UnresolvedReferences, got none")
	}
	var found bool
	for _, ref := range result.UnresolvedReferences {
		if strings.EqualFold(ref.ReferenceName, "users") {
			found = true
			if ref.FromNodeID != ownerID {
				t.Errorf("ref.FromNodeID = %q, want %q", ref.FromNodeID, ownerID)
			}
		}
	}
	if !found {
		t.Errorf("no UnresolvedReference for 'users'; refs = %v", refNames(result.UnresolvedReferences))
	}
}

func TestExtractEmbeddedSQL_ProseReturnsEmpty(t *testing.T) {
	// WHY: Gate failure must produce zero nodes/edges/refs.
	ext := standalone.NewSQLExtractor()
	for _, prose := range []string{
		"choose an item from the dropdown",
		"Copied from the original repo",
	} {
		result := ext.ExtractEmbeddedSQL("/app/main.go", prose, 1, "fn:1")
		if len(result.Nodes) != 0 || len(result.Edges) != 0 || len(result.UnresolvedReferences) != 0 {
			t.Errorf("prose %q: expected empty result, got nodes=%d edges=%d refs=%d",
				prose, len(result.Nodes), len(result.Edges), len(result.UnresolvedReferences))
		}
	}
}

func TestExtractEmbeddedSQL_InterpolatedTableTarget_ZeroRefs(t *testing.T) {
	// Interpolated table target (post-substitution form): harvester replaces
	// the interpolation segment with "?" so no SQL identifier appears after FROM.
	// Result: gate may pass (SELECT + placeholder), but zero table refs and zero
	// nodes are emitted — the placeholder is not a valid SQL identifier.
	// WHY: decision 8 — interpolated table target yields no table ref.
	//
	// F-1 (tightened): use explicit len checks rather than a loop that passes
	// vacuously on an empty slice.
	const literalPostSub = "SELECT a FROM ? WHERE id = %s"
	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/query.go", literalPostSub, 1, "fn:owner:1")

	if len(result.Nodes) != 0 {
		t.Errorf("interpolated-table-target: want 0 nodes, got %d: %v", len(result.Nodes), result.Nodes)
	}
	if len(result.UnresolvedReferences) != 0 {
		t.Errorf("interpolated-table-target: want 0 UnresolvedReferences, got %d: %v",
			len(result.UnresolvedReferences), refNames(result.UnresolvedReferences))
	}
}

func TestExtractEmbeddedSQL_InterpolatedValueLiteralTable_RefToUsers(t *testing.T) {
	// Interpolated value, literal table (post-substitution form): harvester
	// replaces "{id}" with "?" — table name "users" is still plain text.
	// WHY: value interpolation should not suppress the table ref.
	const literalPostSub = "SELECT a FROM users WHERE id = ?"
	const ownerID = "fn:owner:1"

	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/query.go", literalPostSub, 1, ownerID)

	var found bool
	for _, ref := range result.UnresolvedReferences {
		if strings.EqualFold(ref.ReferenceName, "users") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UnresolvedReference for 'users'; refs = %v", refNames(result.UnresolvedReferences))
	}
}

func TestExtractEmbeddedSQL_LineOffset(t *testing.T) {
	// baseLine=10 means the literal starts at line 10 of the host file.
	// Nodes emitted by the SQL extractor (line 1 relative to literal start)
	// should appear at file-absolute line 10.
	// WHY: padding contract — embedded nodes must have file-absolute lines AND
	// IDs. A doubled offset (e.g. calling any post-hoc line adjustment after
	// padding) would produce StartLine=19 instead of 10, and the node ID would
	// be stale relative to the actual StartLine.
	const literal = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	const baseLine = 10
	const file = "/app/migrate.go"

	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL(file, literal, baseLine, "fn:owner:1")

	tableNode := findSQLNode(result.Nodes, types.NodeKindTable, "users")
	if tableNode == nil {
		t.Fatalf("expected table node 'users'")
	}
	if tableNode.StartLine != baseLine {
		t.Errorf("table node StartLine = %d, want %d (baseLine)", tableNode.StartLine, baseLine)
	}

	// Node ID must encode the file-absolute line. Recompute what the ID should
	// be at line=baseLine and assert equality. A doubled offset would give a
	// different line (19) and thus a different hash, catching any re-introduction
	// of post-hoc line adjustment after padding.
	wantID := "table:" + nodeIDHex(file, "table", "users", baseLine)
	if tableNode.ID != wantID {
		t.Errorf("table node ID = %q, want %q (encodes line %d)", tableNode.ID, wantID, baseLine)
	}
}

func TestExtractEmbeddedSQL_MultiLineDDLOffset(t *testing.T) {
	// Multiline DDL at baseLine=5: the table node is on the FIRST line of the
	// literal, so file-absolute StartLine must be exactly 5.
	// WHY: weak < guard allowed a doubled offset (e.g. 9) to pass silently.
	// Exact equality forces the assertion to fail if any post-hoc line
	// adjustment is applied after padding (or if the padding arithmetic is wrong).
	const literal = "CREATE TABLE orders (\n  id SERIAL,\n  user_id INT NOT NULL\n)"
	const baseLine = 5

	ext := standalone.NewSQLExtractor()
	result := ext.ExtractEmbeddedSQL("/app/migrate.go", literal, baseLine, "fn:owner:1")

	tableNode := findSQLNode(result.Nodes, types.NodeKindTable, "orders")
	if tableNode == nil {
		t.Fatalf("expected table node 'orders'")
	}
	if tableNode.StartLine != baseLine {
		t.Errorf("table node StartLine = %d, want exactly %d (file-absolute line of literal start)", tableNode.StartLine, baseLine)
	}
}

// ---------------------------------------------------------------------------
// Node-ID collision test (CP1)
// ---------------------------------------------------------------------------

func TestExtractEmbeddedSQL_DDLNodeIDCollision(t *testing.T) {
	// Two CREATE TABLE users literals at DIFFERENT host-file lines must produce
	// two nodes with DISTINCT IDs.
	//
	// WHY: node IDs are derived from (filePath, kind, name, line). If both
	// literals ran through Extract at literal-relative line 1, both nodes
	// would get the same hash → INSERT OR REPLACE in the DB collapses them
	// to one row. Prepending (baseLine-1) newlines before extraction makes
	// Extract compute file-absolute lines, so the IDs differ.
	//
	// The test must FAIL if either node is dropped (i.e. if the two nodes share
	// an ID after extraction): we collect both IDs and assert they are present
	// and unequal.
	const literal = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	const ownerID = "fn:owner:1"
	const file = "/app/migrate.go"

	ext := standalone.NewSQLExtractor()

	// Literal at line 5 of the host file.
	result1 := ext.ExtractEmbeddedSQL(file, literal, 5, ownerID)
	// Same literal at line 20 of the host file.
	result2 := ext.ExtractEmbeddedSQL(file, literal, 20, ownerID)

	node1 := findSQLNode(result1.Nodes, types.NodeKindTable, "users")
	if node1 == nil {
		t.Fatalf("result1: expected table node 'users', nodes = %v", nodeNames(result1.Nodes))
	}
	node2 := findSQLNode(result2.Nodes, types.NodeKindTable, "users")
	if node2 == nil {
		t.Fatalf("result2: expected table node 'users', nodes = %v", nodeNames(result2.Nodes))
	}

	// Both nodes must be present (non-nil above) AND have distinct IDs.
	if node1.ID == node2.ID {
		t.Errorf("node ID collision: both literals produce ID %q — INSERT OR REPLACE would drop one node", node1.ID)
	}

	// Also verify StartLines are file-absolute (not literal-relative).
	if node1.StartLine != 5 {
		t.Errorf("result1 StartLine = %d, want 5 (file-absolute)", node1.StartLine)
	}
	if node2.StartLine != 20 {
		t.Errorf("result2 StartLine = %d, want 20 (file-absolute)", node2.StartLine)
	}
}

// ---------------------------------------------------------------------------
// ScanBodyEdges exported wrapper test
// ---------------------------------------------------------------------------

func TestScanBodyEdgesExported_Basic(t *testing.T) {
	// ScanBodyEdges is the exported wrapper used by the embedded entry point.
	// It must return an UnresolvedReference for "users" in a FROM clause.
	body := "SELECT id FROM users WHERE active = $1"
	refs := standalone.ScanBodyEdges("/app/query.go", "fn:owner:1", body)

	var found bool
	for _, ref := range refs {
		if strings.EqualFold(ref.ReferenceName, "users") {
			found = true
		}
	}
	if !found {
		t.Errorf("ScanBodyEdges: expected ref for 'users'; refs = %v", refNames(refs))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func nodeNames(nodes []types.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = string(n.Kind) + ":" + n.Name
	}
	return names
}

func refNames(refs []types.UnresolvedReference) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.ReferenceName
	}
	return names
}

// nodeIDHex returns the 32-char hex suffix of the node ID for the given
// (filePath, kind, name, line) — mirrors extraction.GenerateNodeID exactly.
// Used by TestExtractEmbeddedSQL_LineOffset to assert the ID encodes the
// file-absolute line, not a literal-relative one.
func nodeIDHex(filePath, kind, name string, line int) string {
	input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])[:32]
}
