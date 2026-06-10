package extraction_test

// typescript_literals_test.go — unit tests for HarvestTypeScriptLiterals.
//
// WHY: verify template-literal interpolation substitution and plain-string
// harvesting for TypeScript and TSX, independent of the orchestrator pipeline.
// These tests exercise the tree-sitter walk logic directly so regressions
// surface at the unit boundary.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// newTSPool creates a single-instance pool and returns it with a cleanup func.
func newTSPool(t *testing.T) *extraction.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// tsHarvest is a thin helper: borrows an instance, sets the given language, calls
// HarvestTypeScriptLiterals.
func tsHarvest(t *testing.T, pool *extraction.Pool, src string, lang extraction.Lang) []extraction.TSLiteralSpan {
	t.Helper()
	ctx := context.Background()
	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	spans, err := extraction.HarvestTypeScriptLiterals(ctx, inst, src, lang)
	if err != nil {
		t.Fatalf("HarvestTypeScriptLiterals: %v", err)
	}
	return spans
}

// findTSSpan returns the first span whose Text contains substr, or nil.
func findTSSpan(spans []extraction.TSLiteralSpan, substr string) *extraction.TSLiteralSpan {
	for i := range spans {
		if idx := indexOf(spans[i].Text, substr); idx >= 0 {
			return &spans[i]
		}
	}
	return nil
}

func indexOf(s, substr string) int {
	if len(substr) == 0 || len(s) < len(substr) {
		return -1
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// Plain string tests (TypeScript)
// ---------------------------------------------------------------------------

func TestHarvestTypeScriptLiterals_PlainStringContent(t *testing.T) {
	// WHY: single/double-quoted strings must yield their content verbatim.
	pool := newTSPool(t)
	src := `const q = "SELECT id FROM users WHERE active = 1";` + "\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "FROM users")
	if sp == nil {
		t.Fatalf("expected span containing 'FROM users'; got %v", spans)
	}
	want := "SELECT id FROM users WHERE active = 1"
	if sp.Text != want {
		t.Errorf("Text = %q, want %q", sp.Text, want)
	}
}

func TestHarvestTypeScriptLiterals_PlainStringLineNumber(t *testing.T) {
	// WHY: StartLine must be 1-based file-absolute.
	pool := newTSPool(t)
	src := "// line 1\n// line 2\nconst q = \"SELECT a FROM users WHERE id = 1\";\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "FROM users")
	if sp == nil {
		t.Fatal("expected span containing 'FROM users'")
	}
	if sp.StartLine != 3 {
		t.Errorf("StartLine = %d, want 3", sp.StartLine)
	}
}

// ---------------------------------------------------------------------------
// Template literal tests (TypeScript)
// ---------------------------------------------------------------------------

func TestHarvestTypeScriptLiterals_TemplateLiteralNoInterpolation(t *testing.T) {
	// WHY: a template literal without substitutions must yield the full content.
	pool := newTSPool(t)
	src := "const q = `SELECT id FROM orders WHERE paid = 1`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "FROM orders")
	if sp == nil {
		t.Fatalf("expected span containing 'FROM orders'; got %v", spans)
	}
}

func TestHarvestTypeScriptLiterals_TemplateLiteralInterpolatedTable_SubstitutedToPlaceholder(t *testing.T) {
	// WHY: decision 8a — an interpolated table target must produce "?" so no valid
	// SQL identifier appears after FROM. scanBodyEdges emits zero refs for this literal.
	pool := newTSPool(t)
	src := "const q = `SELECT a FROM ${table} WHERE id = ?`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d: %v", len(spans), spans)
	}
	got := spans[0].Text
	const want = "SELECT a FROM ? WHERE id = ?"
	if got != want {
		t.Errorf("post-substitution text = %q, want %q", got, want)
	}
}

func TestHarvestTypeScriptLiterals_TemplateLiteralLiteralTable_PreservesTableName(t *testing.T) {
	// WHY: decision 8b — an interpolated VALUE with a literal table name must
	// preserve the table name so a SQL ref is emitted.
	pool := newTSPool(t)
	src := "const q = `SELECT a FROM users WHERE id = ${id}`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d: %v", len(spans), spans)
	}
	got := spans[0].Text
	const want = "SELECT a FROM users WHERE id = ?"
	if got != want {
		t.Errorf("post-substitution text = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// TSX grammar path
// ---------------------------------------------------------------------------

func TestHarvestTypeScriptLiterals_TSXGrammarPlainString(t *testing.T) {
	// WHY: confirms the TSX grammar path works — same node types, different grammar.
	pool := newTSPool(t)
	// Minimal TSX: a functional component with an embedded SQL string.
	src := `export function Repo() {
  const ddl = "CREATE TABLE widgets (id INT PRIMARY KEY, name TEXT)";
  return <div>{ddl}</div>;
}
`
	spans := tsHarvest(t, pool, src, extraction.LangTSX)

	sp := findTSSpan(spans, "CREATE TABLE widgets")
	if sp == nil {
		t.Fatalf("expected span containing 'CREATE TABLE widgets'; got %v", spans)
	}
}

func TestHarvestTypeScriptLiterals_TSXGrammarTemplateLiteral(t *testing.T) {
	// WHY: confirms template literals are harvested under the TSX grammar.
	pool := newTSPool(t)
	src := "const q = `CREATE TABLE sessions (id INT PRIMARY KEY, token TEXT NOT NULL)`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTSX)

	sp := findTSSpan(spans, "CREATE TABLE sessions")
	if sp == nil {
		t.Fatalf("expected span containing 'CREATE TABLE sessions'; got %v", spans)
	}
}
