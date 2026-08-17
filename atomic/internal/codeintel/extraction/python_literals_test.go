package extraction_test

// Covers docstring exclusion at all three PEP 257 positions and f-string
// substitution, driving the tree-sitter walk directly so a regression surfaces
// here rather than downstream in the orchestrator.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

func newPyPool(t *testing.T) *extraction.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := extraction.NewPool(ctx, extraction.PoolOptions{Size: 1})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func pyHarvest(t *testing.T, pool *extraction.Pool, src string) []extraction.PythonLiteralSpan {
	t.Helper()
	ctx := context.Background()
	inst, err := pool.Borrow(ctx)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	defer pool.Return(inst)

	spans, err := extraction.HarvestPythonLiterals(ctx, inst, src)
	if err != nil {
		t.Fatalf("HarvestPythonLiterals: %v", err)
	}
	return spans
}

// findPySpan returns the first span whose Text contains substr, or nil.
func findPySpan(spans []extraction.PythonLiteralSpan, substr string) *extraction.PythonLiteralSpan {
	for i := range spans {
		s := spans[i].Text
		if len(s) >= len(substr) {
			for j := 0; j+len(substr) <= len(s); j++ {
				if s[j:j+len(substr)] == substr {
					return &spans[i]
				}
			}
		}
	}
	return nil
}

// A bare string at the top of a module is a docstring, not code: prose SQL
// inside it must not become a ref.
func TestHarvestPythonLiterals_ModuleDocstringExcluded(t *testing.T) {
	pool := newPyPool(t)
	src := `"""Module docstring: SELECT * FROM module_secret"""

x = "SELECT a FROM users WHERE id = 1"
`
	spans := pyHarvest(t, pool, src)

	secretSpan := findPySpan(spans, "module_secret")
	if secretSpan == nil {
		t.Fatal("expected span containing 'module_secret' but not found")
	}
	if !secretSpan.IsDocstring {
		t.Error("module-level docstring not marked as IsDocstring=true")
	}

	usersSpan := findPySpan(spans, "FROM users")
	if usersSpan == nil {
		t.Fatal("expected span containing 'FROM users' but not found")
	}
	if usersSpan.IsDocstring {
		t.Error("non-docstring 'FROM users' span incorrectly marked IsDocstring=true")
	}
}

// The class docstring is the first expression_statement in the class body.
func TestHarvestPythonLiterals_ClassDocstringExcluded(t *testing.T) {
	pool := newPyPool(t)
	src := `class Repo:
    """Class docstring: SELECT * FROM class_secret"""
    def method(self):
        q = "SELECT a FROM users WHERE id = 1"
`
	spans := pyHarvest(t, pool, src)

	secretSpan := findPySpan(spans, "class_secret")
	if secretSpan == nil {
		t.Fatal("expected span containing 'class_secret'")
	}
	if !secretSpan.IsDocstring {
		t.Error("class docstring not marked IsDocstring=true")
	}

	usersSpan := findPySpan(spans, "FROM users")
	if usersSpan == nil {
		t.Fatal("expected span containing 'FROM users'")
	}
	if usersSpan.IsDocstring {
		t.Error("non-docstring method literal incorrectly marked IsDocstring=true")
	}
}

// Likewise the first expression_statement in a function body.
func TestHarvestPythonLiterals_FunctionDocstringExcluded(t *testing.T) {
	pool := newPyPool(t)
	src := `def run():
    """Function docstring: CREATE TABLE fn_secret (id INT)"""
    q = "SELECT a FROM users WHERE id = 1"
`
	spans := pyHarvest(t, pool, src)

	secretSpan := findPySpan(spans, "fn_secret")
	if secretSpan == nil {
		t.Fatal("expected span containing 'fn_secret'")
	}
	if !secretSpan.IsDocstring {
		t.Error("function docstring not marked IsDocstring=true")
	}

	usersSpan := findPySpan(spans, "FROM users")
	if usersSpan == nil {
		t.Fatal("expected span containing 'FROM users'")
	}
	if usersSpan.IsDocstring {
		t.Error("non-docstring literal inside function incorrectly marked IsDocstring=true")
	}
}

// Substituting "?" leaves no valid identifier after FROM, so the SQL gate may
// still pass the string while scanBodyEdges emits zero refs from it.
func TestHarvestPythonLiterals_FStringInterpolatedTable_SubstitutedToPlaceholder(t *testing.T) {
	pool := newPyPool(t)
	src := `q = f"SELECT a FROM {table} WHERE id = %s"
`
	spans := pyHarvest(t, pool, src)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	got := spans[0].Text
	const want = "SELECT a FROM ? WHERE id = %s"
	if got != want {
		t.Errorf("post-substitution text = %q, want %q", got, want)
	}
}

// Interpolating a value must leave the literal table name beside it intact,
// or the SQL ref is lost with it.
func TestHarvestPythonLiterals_FStringLiteralTable_PreservesTableName(t *testing.T) {
	pool := newPyPool(t)
	src := `q = f"SELECT a FROM users WHERE id = {uid}"
`
	spans := pyHarvest(t, pool, src)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	got := spans[0].Text
	const want = "SELECT a FROM users WHERE id = ?"
	if got != want {
		t.Errorf("post-substitution text = %q, want %q", got, want)
	}
}

// StartLine is file-absolute and 1-based.
func TestHarvestPythonLiterals_RegularStringLineNumbers(t *testing.T) {
	pool := newPyPool(t)
	src := `# line 1
# line 2
q = "SELECT a FROM users WHERE id = 1"
`
	spans := pyHarvest(t, pool, src)
	usersSpan := findPySpan(spans, "FROM users")
	if usersSpan == nil {
		t.Fatal("expected span containing 'FROM users'")
	}
	if usersSpan.StartLine != 3 {
		t.Errorf("StartLine=%d, want 3", usersSpan.StartLine)
	}
}

// A triple-quoted string in assignment position carries most multi-line SQL in
// Python, so it must be harvested rather than mistaken for a docstring.
func TestHarvestPythonLiterals_TripleQuotedString(t *testing.T) {
	pool := newPyPool(t)
	src := `q = """
CREATE TABLE orders (id SERIAL PRIMARY KEY)
"""
`
	spans := pyHarvest(t, pool, src)
	ordersSpan := findPySpan(spans, "orders")
	if ordersSpan == nil {
		t.Fatalf("expected span containing 'orders'; got spans: %v", spans)
	}
	if ordersSpan.IsDocstring {
		t.Error("assignment string incorrectly marked as docstring")
	}
}

// calleeCtx applies only to a call's "arguments" subtree: here the literal sits
// in the receiver position, so it must not inherit "upper".
func TestHarvestPythonLiterals_CalleeExprScopedToArguments(t *testing.T) {
	pool := newPyPool(t)
	src := "\"tbl\".upper()\n"
	spans := pyHarvest(t, pool, src)

	sp := findPySpan(spans, "tbl")
	if sp == nil {
		t.Fatalf("expected span containing 'tbl'; got %v", spans)
	}
	if sp.CalleeExpr != "" {
		t.Errorf("CalleeExpr = %q, want empty (literal is in receiver position, not arguments)", sp.CalleeExpr)
	}
}

// Control for the case above: inside the arguments list, the bare callee name
// is still captured.
func TestHarvestPythonLiterals_CalleeExprSetForArgument(t *testing.T) {
	pool := newPyPool(t)
	src := "db.select_from(\"orders_view\")\n"
	spans := pyHarvest(t, pool, src)

	sp := findPySpan(spans, "orders_view")
	if sp == nil {
		t.Fatalf("expected span containing 'orders_view'; got %v", spans)
	}
	if sp.CalleeExpr != "select_from" {
		t.Errorf("CalleeExpr = %q, want %q", sp.CalleeExpr, "select_from")
	}
}
