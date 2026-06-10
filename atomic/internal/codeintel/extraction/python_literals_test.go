package extraction_test

// python_literals_test.go — unit tests for HarvestPythonLiterals.
//
// WHY: verify docstring exclusion at all three PEP 257 positions (module,
// class, function) and f-string interpolation substitution, independent of the
// orchestrator pipeline. These tests exercise the tree-sitter walk logic
// directly so regressions surface at the unit boundary.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// newPyPool creates a single-instance pool and returns it with a cleanup
// function. Shared across subtests within a test function.
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

// pyHarvest is a thin helper that borrows an instance, sets Python, and calls
// HarvestPythonLiterals, returning the slice (or nil on error).
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

// findSpan returns the first span whose Text contains substr, or nil.
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

// ---------------------------------------------------------------------------
// Docstring exclusion tests
// ---------------------------------------------------------------------------

func TestHarvestPythonLiterals_ModuleDocstringExcluded(t *testing.T) {
	// WHY: a bare string at the top of a module is a module-level docstring
	// (PEP 257). SQL inside it must be excluded (decision 4).
	pool := newPyPool(t)
	src := `"""Module docstring: SELECT * FROM module_secret"""

x = "SELECT a FROM users WHERE id = 1"
`
	spans := pyHarvest(t, pool, src)

	// "module_secret" must be marked as docstring.
	secretSpan := findPySpan(spans, "module_secret")
	if secretSpan == nil {
		t.Fatal("expected span containing 'module_secret' but not found")
	}
	if !secretSpan.IsDocstring {
		t.Error("module-level docstring not marked as IsDocstring=true")
	}

	// The non-docstring "users" span must NOT be a docstring.
	usersSpan := findPySpan(spans, "FROM users")
	if usersSpan == nil {
		t.Fatal("expected span containing 'FROM users' but not found")
	}
	if usersSpan.IsDocstring {
		t.Error("non-docstring 'FROM users' span incorrectly marked IsDocstring=true")
	}
}

func TestHarvestPythonLiterals_ClassDocstringExcluded(t *testing.T) {
	// WHY: first expression_statement in a class body is the class docstring.
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

func TestHarvestPythonLiterals_FunctionDocstringExcluded(t *testing.T) {
	// WHY: first expression_statement in a function body is the function docstring.
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

// ---------------------------------------------------------------------------
// F-string substitution tests
// ---------------------------------------------------------------------------

func TestHarvestPythonLiterals_FStringInterpolatedTable_SubstitutedToPlaceholder(t *testing.T) {
	// WHY: decision 8a — an interpolated table target must yield a post-substitution
	// text with "?" in place of the interpolation, so no valid SQL identifier
	// appears after FROM. The SQL gate may pass, but scanBodyEdges emits zero refs.
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

func TestHarvestPythonLiterals_FStringLiteralTable_PreservesTableName(t *testing.T) {
	// WHY: decision 8b — an interpolated VALUE with a literal table name must
	// preserve the table name so a SQL ref is emitted.
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

// ---------------------------------------------------------------------------
// Line number tests
// ---------------------------------------------------------------------------

func TestHarvestPythonLiterals_RegularStringLineNumbers(t *testing.T) {
	// WHY: StartLine must be file-absolute (1-based).
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

func TestHarvestPythonLiterals_TripleQuotedString(t *testing.T) {
	// WHY: triple-quoted strings must be harvested; they are common for
	// multi-line SQL in Python.
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
