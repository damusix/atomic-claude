package extraction_test

// CalleeExpr feeds the query-builder vocabulary that promotes a SQL string
// match from medium to high confidence. See docs/spec/sql-string-match.md.

import "testing"

func TestHarvestPythonLiterals_CalleeCapture(t *testing.T) {
	pool := newPyPool(t)

	src := `
a = db.select("worker_document")
b = "not_in_a_call"
c = query("orders")
d = nested(other("prose string"))
`
	spans := pyHarvest(t, pool, src)

	t.Run("attribute call — bare attribute name", func(t *testing.T) {
		s := findPySpan(spans, "worker_document")
		if s == nil {
			t.Fatal("expected span for worker_document")
		}
		if s.CalleeExpr != "select" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "select")
		}
	})

	t.Run("not in a call — empty CalleeExpr", func(t *testing.T) {
		s := findPySpan(spans, "not_in_a_call")
		if s == nil {
			t.Fatal("expected span for not_in_a_call")
		}
		if s.CalleeExpr != "" {
			t.Errorf("CalleeExpr = %q, want empty", s.CalleeExpr)
		}
	})

	t.Run("plain identifier callee", func(t *testing.T) {
		s := findPySpan(spans, "orders")
		if s == nil {
			t.Fatal("expected span for orders")
		}
		if s.CalleeExpr != "query" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "query")
		}
	})

	t.Run("nearest enclosing call wins for nested calls", func(t *testing.T) {
		s := findPySpan(spans, "prose string")
		if s == nil {
			t.Fatal("expected span for prose string")
		}
		if s.CalleeExpr != "other" {
			t.Errorf("CalleeExpr = %q, want %q (nearest enclosing, not %q)", s.CalleeExpr, "other", "nested")
		}
	})
}
