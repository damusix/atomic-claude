package extraction_test

// typescript_callee_test.go — unit tests for HarvestTypeScriptLiterals'
// callee capture (sql-string-match C1).

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

func TestHarvestTypeScriptLiterals_CalleeCapture(t *testing.T) {
	pool := newTSPool(t)

	src := `
const a = db.selectFrom("worker_document");
const b = "not_in_a_call";
const c = query("orders");
const d = nested(other("prose string"));
`
	spans := tsHarvest(t, pool, src, extraction.LangTypeScript)

	t.Run("member call — bare property name", func(t *testing.T) {
		s := findTSSpan(spans, "worker_document")
		if s == nil {
			t.Fatal("expected span for worker_document")
		}
		if s.CalleeExpr != "selectFrom" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "selectFrom")
		}
	})

	t.Run("not in a call — empty CalleeExpr", func(t *testing.T) {
		s := findTSSpan(spans, "not_in_a_call")
		if s == nil {
			t.Fatal("expected span for not_in_a_call")
		}
		if s.CalleeExpr != "" {
			t.Errorf("CalleeExpr = %q, want empty", s.CalleeExpr)
		}
	})

	t.Run("plain identifier callee", func(t *testing.T) {
		s := findTSSpan(spans, "orders")
		if s == nil {
			t.Fatal("expected span for orders")
		}
		if s.CalleeExpr != "query" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "query")
		}
	})

	t.Run("nearest enclosing call wins for nested calls", func(t *testing.T) {
		s := findTSSpan(spans, "prose string")
		if s == nil {
			t.Fatal("expected span for prose string")
		}
		if s.CalleeExpr != "other" {
			t.Errorf("CalleeExpr = %q, want %q (nearest enclosing, not %q)", s.CalleeExpr, "other", "nested")
		}
	})
}
