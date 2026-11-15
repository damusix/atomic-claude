package doctor_test

import (
	"io"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// A ledger-managed category repairs through the converge seam, and the fix loop
// still prompts once per item: a Yes converges, a No skips the next.
func TestRepair_ConvergePromptsPerItem(t *testing.T) {
	home := t.TempDir()
	var calls []string

	rp := nopRepairer()
	rp.HomeFn = func() (string, error) { return home, nil }
	rp.ConvergeFn = func(h string, _ io.Writer) error {
		calls = append(calls, h)
		return nil
	}

	results := []doctor.Result{
		makeResult(21, "staleness", doctor.WARN, "materialized generation is behind the selected projection"),
		makeResult(15, "targets", doctor.WARN, "native root is not present"),
	}
	p := &fakePrompter{decisions: []doctor.Decision{doctor.DecisionYes, doctor.DecisionNo}}

	var out strings.Builder
	summary := rp.Repair(results, doctor.Opts{Fix: true}, p, &out)

	if summary.Applied != 1 || summary.Skipped != 1 || summary.NonFixable != 0 {
		t.Fatalf("summary = %+v, want 1 applied, 1 skipped", summary)
	}
	if len(calls) != 1 || calls[0] != home {
		t.Fatalf("converge calls = %v, want one against %s", calls, home)
	}
}

// A report-only category is non-fixable: the loop must not route it through the
// converge seam even though it carries a WARN.
func TestRepair_ReportOnlyCategoryIsNonFixable(t *testing.T) {
	rp := nopRepairer()
	rp.ConvergeFn = func(string, io.Writer) error {
		t.Fatal("converge ran for a report-only category")
		return nil
	}

	results := []doctor.Result{
		makeResult(18, "capabilities", doctor.WARN, "rule-delivery roles unproven"),
		makeResult(22, "conflicts", doctor.WARN, "resource carries bytes Atomic did not record"),
	}

	var out strings.Builder
	summary := rp.Repair(results, doctor.Opts{Fix: true}, &fakePrompter{}, &out)

	if summary.NonFixable != 2 || summary.Applied != 0 || summary.Skipped != 0 {
		t.Fatalf("summary = %+v, want both report-only categories non-fixable", summary)
	}
}
