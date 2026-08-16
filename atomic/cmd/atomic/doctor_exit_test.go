package main

import (
	"errors"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// The contract these tests pin: `atomic doctor --fix` must exit on what is true
// after repairs, not on the verdict that triggered them. A caller gating CI on
// the exit code has to be able to distinguish "found problems and fixed them"
// from "still broken"; reporting the pre-repair code collapses both to 1.

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
