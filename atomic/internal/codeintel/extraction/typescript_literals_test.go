package extraction_test

// Covers template-literal substitution and plain-string harvesting under both
// the TypeScript and TSX grammars, driving the tree-sitter walk directly so a
// regression surfaces here rather than downstream in the orchestrator.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

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

// Quoted strings yield their content verbatim — no delimiters, no rewriting.
func TestHarvestTypeScriptLiterals_PlainStringContent(t *testing.T) {
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

// StartLine is file-absolute and 1-based.
func TestHarvestTypeScriptLiterals_PlainStringLineNumber(t *testing.T) {
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

// A template literal with no substitutions yields its full content unaltered.
func TestHarvestTypeScriptLiterals_TemplateLiteralNoInterpolation(t *testing.T) {
	pool := newTSPool(t)
	src := "const q = `SELECT id FROM orders WHERE paid = 1`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "FROM orders")
	if sp == nil {
		t.Fatalf("expected span containing 'FROM orders'; got %v", spans)
	}
}

// Substituting "?" leaves no valid identifier after FROM, so scanBodyEdges
// emits zero refs from this literal.
func TestHarvestTypeScriptLiterals_TemplateLiteralInterpolatedTable_SubstitutedToPlaceholder(t *testing.T) {
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

// Interpolating a value must leave the literal table name beside it intact,
// or the SQL ref is lost with it.
func TestHarvestTypeScriptLiterals_TemplateLiteralLiteralTable_PreservesTableName(t *testing.T) {
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

// TSX is a separate grammar that happens to share these node types; the harvest
// path must hold across both.
func TestHarvestTypeScriptLiterals_TSXGrammarPlainString(t *testing.T) {
	pool := newTSPool(t)
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
	pool := newTSPool(t)
	src := "const q = `CREATE TABLE sessions (id INT PRIMARY KEY, token TEXT NOT NULL)`;\n"
	spans := tsHarvest(t, pool, src, extraction.LangTSX)

	sp := findTSSpan(spans, "CREATE TABLE sessions")
	if sp == nil {
		t.Fatalf("expected span containing 'CREATE TABLE sessions'; got %v", spans)
	}
}

// calleeCtx applies only to a call's "arguments" subtree: here the literal sits
// in the receiver position, so it must not inherit "toUpperCase".
func TestHarvestTypeScriptLiterals_CalleeExprScopedToArguments(t *testing.T) {
	pool := newTSPool(t)
	src := `"tbl".toUpperCase();` + "\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "tbl")
	if sp == nil {
		t.Fatalf("expected span containing 'tbl'; got %v", spans)
	}
	if sp.CalleeExpr != "" {
		t.Errorf("CalleeExpr = %q, want empty (literal is in receiver position, not arguments)", sp.CalleeExpr)
	}
}

// Control for the case above: inside the arguments list, the bare callee name
// is still captured.
func TestHarvestTypeScriptLiterals_CalleeExprSetForArgument(t *testing.T) {
	pool := newTSPool(t)
	src := `db.selectFrom("orders_view");` + "\n"
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	sp := findTSSpan(spans, "orders_view")
	if sp == nil {
		t.Fatalf("expected span containing 'orders_view'; got %v", spans)
	}
	if sp.CalleeExpr != "selectFrom" {
		t.Errorf("CalleeExpr = %q, want %q", sp.CalleeExpr, "selectFrom")
	}
}
