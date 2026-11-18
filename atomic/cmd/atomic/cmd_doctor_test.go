package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// `doctor --fix` must exit on what is true after repairs, not on the verdict
// that triggered them.

func TestPostRepairExitCode_noRepairsKeepsOriginalVerdict(t *testing.T) {
	called := false
	got := postRepairExitCode(1, 0, func() ([]doctor.Result, error) {
		called = true
		return nil, nil
	})
	if got != 1 {
		t.Errorf("exit code: got %d, want 1 (nothing was repaired, so the verdict stands)", got)
	}
	if called {
		t.Error("re-check ran despite zero repairs; the second pass is only worth its cost after a repair")
	}
}

func TestPostRepairExitCode_repairedStateReportsSuccess(t *testing.T) {
	got := postRepairExitCode(1, 2, func() ([]doctor.Result, error) {
		return []doctor.Result{{Severity: doctor.PASS}, {Severity: doctor.WARN}}, nil
	})
	if got != 0 {
		t.Errorf("exit code: got %d, want 0 (repairs cleared the FAIL)", got)
	}
}

func TestPostRepairExitCode_stillFailingAfterRepairStaysNonZero(t *testing.T) {
	got := postRepairExitCode(1, 1, func() ([]doctor.Result, error) {
		return []doctor.Result{{Severity: doctor.PASS}, {Severity: doctor.FAIL}}, nil
	})
	if got != 1 {
		t.Errorf("exit code: got %d, want 1 (a repair landed but something still fails)", got)
	}
}

func TestPostRepairExitCode_failedRecheckDoesNotClaimSuccess(t *testing.T) {
	got := postRepairExitCode(1, 1, func() ([]doctor.Result, error) {
		return nil, errors.New("re-check blew up")
	})
	if got != 1 {
		t.Errorf("exit code: got %d, want 1 — an unobservable state must not be reported as healthy", got)
	}
}

// An OMP session on an empty home is told to install OMP, not Claude.
func TestDoctorShortCircuitNamesTheDetectedHarness(t *testing.T) {
	out, code := runAtomicCLIEnv(t, t.TempDir(), []string{"AGENT=1", "CI=true"}, "root", "doctor")
	if code != 0 || !strings.Contains(out, "atomic install --harness omp") {
		t.Fatalf("doctor = exit %d, %q; want exit 0 naming the omp install", code, out)
	}
}
