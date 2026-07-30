package standalone_test

// go_harvester_test.go — unit tests for HarvestGoStringLiterals' callee
// capture (sql-string-match C1): the bare name of the nearest enclosing call
// expression whose argument list contains the literal.
//
// WHY: the Go harvester is a hand-written scanner (no AST), so callee capture
// is a heuristic paren-stack tracker — these tests pin its behavior directly,
// independent of the postpass/resolution pipeline.

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/standalone"
)

func findGoSpan(spans []standalone.StringLiteralSpan, text string) *standalone.StringLiteralSpan {
	for i := range spans {
		if spans[i].Text == text {
			return &spans[i]
		}
	}
	return nil
}

func TestHarvestGoStringLiterals_CalleeCapture(t *testing.T) {
	src := `package x

func f() {
	db.SelectFrom("worker_document")
	plain := "not_in_a_call"
	fmt.Println("count", plain)
	if x := db.Query("orders"); x != nil {
	}
}
`
	spans := standalone.HarvestGoStringLiterals(src)

	t.Run("in call — qualified receiver bare name", func(t *testing.T) {
		s := findGoSpan(spans, "worker_document")
		if s == nil {
			t.Fatal("expected span for worker_document")
		}
		if s.CalleeExpr != "SelectFrom" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "SelectFrom")
		}
	})

	t.Run("not in a call — empty CalleeExpr", func(t *testing.T) {
		s := findGoSpan(spans, "not_in_a_call")
		if s == nil {
			t.Fatal("expected span for not_in_a_call")
		}
		if s.CalleeExpr != "" {
			t.Errorf("CalleeExpr = %q, want empty", s.CalleeExpr)
		}
	})

	t.Run("in call — plain identifier callee", func(t *testing.T) {
		s := findGoSpan(spans, "count")
		if s == nil {
			t.Fatal("expected span for count")
		}
		if s.CalleeExpr != "Println" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "Println")
		}
	})

	t.Run("call inside an if-statement condition", func(t *testing.T) {
		s := findGoSpan(spans, "orders")
		if s == nil {
			t.Fatal("expected span for orders")
		}
		if s.CalleeExpr != "Query" {
			t.Errorf("CalleeExpr = %q, want %q", s.CalleeExpr, "Query")
		}
	})
}
