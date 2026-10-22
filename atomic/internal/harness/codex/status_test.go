package codex

import (
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// TestRuntimeStateReportsEverySurfaceUnproven pins the honesty contract: every
// runtime surface the doctor reads is reported with the evidence that fixes it,
// and none of them reads as support. Registration is reported separately and
// only from observed bytes.
func TestRuntimeStateReportsEverySurfaceUnproven(t *testing.T) {
	home := newHome(t)
	root := newRoot(t, home)
	a := &Adapter{}

	rows := a.RuntimeState(root)
	rows = append(rows, a.PayloadState()...)
	rows = append(rows, a.ChildState()...)
	rows = append(rows, a.LastProofState()...)
	if len(rows) == 0 {
		t.Fatal("no runtime surfaces reported")
	}
	for _, row := range rows {
		if row.Surface == "" || row.Evidence == "" {
			t.Errorf("surface %+v is missing its identity or evidence", row)
		}
		if row.Status != harness.StatusUnsupported {
			t.Errorf("surface %q reports %s; no Codex runtime row is proven for 0.147.0", row.Surface, row.Status)
		}
	}

	// The payload row names Atomic's own bound and says it is not native.
	found := false
	for _, row := range rows {
		if row.Surface == "instruction and payload limits" {
			found = true
			if !strings.Contains(row.Evidence, "not a native limit") {
				t.Errorf("payload bound evidence %q does not disclaim the native limit", row.Evidence)
			}
		}
	}
	if !found {
		t.Error("payload-limit surface not reported")
	}

	// Registration is unproven until Codex's own file records it.
	row := a.RegistrationStateRow(root)
	if row.Surface == "" || row.Status != harness.StatusUnsupported {
		t.Errorf("registration row = %+v, want an unsupported observation for an absent registry", row)
	}

	gap := RuntimeDisablementGap()
	if gap.Surface == "" || gap.Evidence == "" {
		t.Errorf("disablement gap = %+v, want a named surface with evidence", gap)
	}
}
