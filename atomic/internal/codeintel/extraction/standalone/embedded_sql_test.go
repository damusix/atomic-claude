package standalone_test

// Substitution contract: harvesters replace language-specific interpolation with
// placeholders before calling ExtractEmbeddedSQL. An interpolated TABLE TARGET must
// lose its identifier, so nothing recognizable follows FROM/JOIN and no table ref is
// emitted; an interpolated VALUE becomes a SQL placeholder and the table ref stands.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

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
			// The zero refs come from ExtractEmbeddedSQL, not from the gate.
			name:    "interpolated table target post-substitution - gate passes (FROM present, placeholder present)",
			input:   "SELECT a FROM  WHERE id = %s",
			wantSQL: true,
		},
		{
			name:    "interpolated value literal table post-substitution passes",
			input:   "SELECT a FROM users WHERE id = ?",
			wantSQL: true,
		},
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

// UPDATE admits only alongside a SET token: prose like "UPDATE available: version
// %s" carries the verb and a confidence discriminator but never SET.
func TestIsSQLLiteral_UpdateSETGate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSQL bool
	}{
		{
			name:    "UPDATE prose with placeholder but no SET rejects",
			input:   "UPDATE available: version %s",
			wantSQL: false,
		},
		{
			// %d is not in dmlConfidenceRE but the bare = is — the real false positive.
			name:    "UPDATE prose with comparison but no SET rejects",
			input:   "UPDATE plan len = %d",
			wantSQL: false,
		},
		{
			name:    "real UPDATE with SET admits",
			input:   "UPDATE users SET name = $1 WHERE id = $2",
			wantSQL: true,
		},
		// Other DML verbs stay unaffected by the UPDATE/SET guard.
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

func TestExtractEmbeddedSQL_DDLEmitsTableNode(t *testing.T) {
	// The DDL path reuses SQLExtractor.Extract verbatim.
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
	// Provenance is what makes these edges reachable via GetEdgesByProvenance.
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
	// The DML path routes to scanBodyEdges rather than Extract.
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
	// Post-substitution form: the placeholder in the FROM slot is not a valid SQL
	// identifier, so the gate can pass while zero nodes and zero refs come out.
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
	// Value interpolation must not suppress the table ref.
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
	// Padding contract: embedded nodes carry file-absolute lines and IDs. A line
	// adjustment applied after padding doubles the offset — StartLine 19, not 10.
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

	// The ID hashes the line, so a doubled offset shifts the hash too.
	wantID := "table:" + nodeIDHex(file, "table", "users", baseLine)
	if tableNode.ID != wantID {
		t.Errorf("table node ID = %q, want %q (encodes line %d)", tableNode.ID, wantID, baseLine)
	}
}

func TestExtractEmbeddedSQL_MultiLineDDLOffset(t *testing.T) {
	// The table node sits on the literal's first line, so StartLine is exactly
	// baseLine. A `<` guard here let a doubled offset pass silently.
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

func TestExtractEmbeddedSQL_DDLNodeIDCollision(t *testing.T) {
	// Node IDs hash (filePath, kind, name, line). Without the newline padding both
	// literals extract at literal-relative line 1, collide on ID, and INSERT OR
	// REPLACE collapses them into one row.
	const literal = "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT NOT NULL)"
	const ownerID = "fn:owner:1"
	const file = "/app/migrate.go"

	ext := standalone.NewSQLExtractor()

	result1 := ext.ExtractEmbeddedSQL(file, literal, 5, ownerID)
	result2 := ext.ExtractEmbeddedSQL(file, literal, 20, ownerID)

	node1 := findSQLNode(result1.Nodes, types.NodeKindTable, "users")
	if node1 == nil {
		t.Fatalf("result1: expected table node 'users', nodes = %v", nodeNames(result1.Nodes))
	}
	node2 := findSQLNode(result2.Nodes, types.NodeKindTable, "users")
	if node2 == nil {
		t.Fatalf("result2: expected table node 'users', nodes = %v", nodeNames(result2.Nodes))
	}

	if node1.ID == node2.ID {
		t.Errorf("node ID collision: both literals produce ID %q — INSERT OR REPLACE would drop one node", node1.ID)
	}

	if node1.StartLine != 5 {
		t.Errorf("result1 StartLine = %d, want 5 (file-absolute)", node1.StartLine)
	}
	if node2.StartLine != 20 {
		t.Errorf("result2 StartLine = %d, want 20 (file-absolute)", node2.StartLine)
	}
}

func TestScanBodyEdgesExported_Basic(t *testing.T) {
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

// Mirrors extraction.GenerateNodeID; must stay in step with it.
func nodeIDHex(filePath, kind, name string, line int) string {
	input := fmt.Sprintf("%s:%s:%s:%d", filePath, kind, name, line)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])[:32]
}
