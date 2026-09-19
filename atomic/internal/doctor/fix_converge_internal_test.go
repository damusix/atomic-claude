package doctor

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// A ledger-managed repair reuses the CP7A converge planner: it resolves
// enrolled targets, projects them, and hands convergence to the adapter.
func TestDefaultConvergeRepairRoutesThroughPlanner(t *testing.T) {
	home, root, _ := claudeLedgerHome(t)
	target := claudeTarget(home, root)
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)

	var converged []string
	restore := convergeStepsFn
	convergeStepsFn = func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{
			kind:      harness.KindClaude,
			instances: []harness.Instance{claudeInstance(home, root)},
			onConverge: func(_ string, t harness.Target) {
				converged = append(converged, t.Key())
			},
		})
	}
	t.Cleanup(func() { convergeStepsFn = restore })

	var out strings.Builder
	if err := defaultConvergeRepair(home, &out); err != nil {
		t.Fatalf("defaultConvergeRepair: %v", err)
	}
	if len(converged) != 1 || converged[0] != target.Key() {
		t.Fatalf("converged = %v, want [%s]", converged, target.Key())
	}
}

// With nothing enrolled the converge planner has no target to act on, which the
// fix loop reports as non-fixable rather than as a failed repair.
func TestDefaultConvergeRepairWithoutTargetsIsNonFixable(t *testing.T) {
	home := t.TempDir()
	err := defaultConvergeRepair(home, io.Discard)
	if err != errNonFixable {
		t.Fatalf("err = %v, want errNonFixable", err)
	}
}

// A target the planner refuses to mutate (a blocker) is not a repair. The
// converge repair must return errNonFixable so the fix loop counts it
// non-fixable instead of reporting "fixed" for an untouched state — the same
// convention the install verb uses when a blocked target fails convergence.
func TestDefaultConvergeRepairBlockerIsNonFixable(t *testing.T) {
	home, root, _ := claudeLedgerHome(t)
	target := claudeTarget(home, root)
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)

	restore := convergeStepsFn
	convergeStepsFn = func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{
			kind:      harness.KindClaude,
			instances: []harness.Instance{claudeInstance(home, root)},
			plan:      harness.Plan{Blockers: []string{"ambiguous managed block"}},
			onConverge: func(string, harness.Target) {
				t.Error("a blocked plan was converged")
			},
		})
	}
	t.Cleanup(func() { convergeStepsFn = restore })

	var out strings.Builder
	err := defaultConvergeRepair(home, &out)
	if err != errNonFixable {
		t.Fatalf("err = %v, want errNonFixable", err)
	}
	if !strings.Contains(out.String(), "ambiguous managed block") {
		t.Fatalf("out = %q, want the blocker printed", out.String())
	}
}

// A doctor converge repair serializes against update and uninstall through the
// one advisory lifecycle lock: while another lifecycle operation holds the lock,
// the repair blocks, and it completes once the lock is released.
func TestDefaultConvergeRepairSerializesWithLifecycleLock(t *testing.T) {
	home, root, _ := claudeLedgerHome(t)
	target := claudeTarget(home, root)
	saveLedger(t, home, []installstate.TargetRecord{target}, nil)

	holder, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "concurrent-update"})
	if err != nil {
		t.Fatalf("acquire lifecycle lock: %v", err)
	}

	restore := convergeStepsFn
	convergeStepsFn = func(h string) install.Steps {
		return stepsWith(h, fakeAdapter{
			kind:      harness.KindClaude,
			instances: []harness.Instance{claudeInstance(home, root)},
			// The adapter takes the same lock every lifecycle verb takes.
			onConverge: func(home string, _ harness.Target) {
				lock, err := installstate.AcquireLock(home, installstate.WriterIdentity{OperationID: "doctor-converge"})
				if err != nil {
					t.Errorf("adapter acquire lock: %v", err)
					return
				}
				_ = lock.Release()
			},
		})
	}
	t.Cleanup(func() { convergeStepsFn = restore })

	done := make(chan error, 1)
	go func() { done <- defaultConvergeRepair(home, io.Discard) }()

	select {
	case err := <-done:
		t.Fatalf("repair completed while the lifecycle lock was held (err=%v)", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := holder.Release(); err != nil {
		t.Fatalf("release lifecycle lock: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("repair after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("repair did not complete after the lifecycle lock was released")
	}
}
