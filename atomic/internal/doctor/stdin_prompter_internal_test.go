package doctor

// Tests for stdinPrompter that require access to unexported fields.
// Kept separate from fix_test.go (package doctor_test) so the confirmFn
// field can be injected directly without exporting it.

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// newTestPrompter builds a stdinPrompter with an injected confirmFn and an
// optional piped reader for the raw fallback path.
func newTestPrompter(raw string, fn func(string, string, bool) (bool, error)) *stdinPrompter {
	return &stdinPrompter{
		scanner:   bufio.NewScanner(strings.NewReader(raw)),
		out:       io.Discard,
		confirmFn: fn,
	}
}

// TestStdinPrompter_ErrAborted_returnsDecisionAbort verifies the
// ErrAborted → DecisionAbort mapping in stdinPrompter.Confirm.
//
// WHY this test is non-vacuous: if the `errors.Is(err, prompt.ErrAborted)`
// branch (lines 47-49 of stdin_prompter.go) is deleted, the prompter falls
// through to the raw line-based path, reads from the empty scanner, and
// returns DecisionNo — not DecisionAbort. This assertion catches that deletion.
func TestStdinPrompter_ErrAborted_returnsDecisionAbort(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return false, prompt.ErrAborted
	})
	got := p.Confirm("Continue?")
	if got != DecisionAbort {
		t.Errorf("expected DecisionAbort, got %v", got)
	}
}

// TestStdinPrompter_ErrNonInteractive_fallsBackToRawInput verifies that when
// confirmFn returns ErrNonInteractive, the prompter uses the raw line reader.
//
// WHY: ensures the fallback path is intact and that ErrNonInteractive is not
// mis-routed as DecisionAbort or an error. If the fallback branch were removed,
// this would return DecisionNo (EOF) rather than DecisionYes.
func TestStdinPrompter_ErrNonInteractive_fallsBackToRawInput(t *testing.T) {
	p := newTestPrompter("y\n", func(_, _ string, _ bool) (bool, error) {
		return false, prompt.ErrNonInteractive
	})
	got := p.Confirm("Continue? ")
	if got != DecisionYes {
		t.Errorf("expected DecisionYes from raw 'y', got %v", got)
	}
}

// TestStdinPrompter_ConfirmSuccess_returnsDecisionYes verifies the happy path:
// confirmFn returns (true, nil) → DecisionYes.
func TestStdinPrompter_ConfirmSuccess_returnsDecisionYes(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return true, nil
	})
	got := p.Confirm("Proceed?")
	if got != DecisionYes {
		t.Errorf("expected DecisionYes, got %v", got)
	}
}

// TestStdinPrompter_ConfirmFalse_returnsDecisionNo verifies confirmFn
// succeeding with false yields DecisionNo.
func TestStdinPrompter_ConfirmFalse_returnsDecisionNo(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return false, nil
	})
	got := p.Confirm("Proceed?")
	if got != DecisionNo {
		t.Errorf("expected DecisionNo, got %v", got)
	}
}
