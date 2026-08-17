package doctor

// In-package so confirmFn can be injected without exporting it.

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// newTestPrompter injects confirmFn plus a reader for the raw fallback path.
func newTestPrompter(raw string, fn func(string, string, bool) (bool, error)) *stdinPrompter {
	return &stdinPrompter{
		scanner:   bufio.NewScanner(strings.NewReader(raw)),
		out:       io.Discard,
		confirmFn: fn,
	}
}

// Non-vacuous because the scanner is empty: drop the ErrAborted branch and
// the raw fallback returns DecisionNo instead.
func TestStdinPrompter_ErrAborted_returnsDecisionAbort(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return false, prompt.ErrAborted
	})
	got := p.Confirm("Continue?")
	if got != DecisionAbort {
		t.Errorf("expected DecisionAbort, got %v", got)
	}
}

// The DecisionYes here can only come from the raw reader, so removing the
// fallback branch turns this into DecisionNo at EOF.
func TestStdinPrompter_ErrNonInteractive_fallsBackToRawInput(t *testing.T) {
	p := newTestPrompter("y\n", func(_, _ string, _ bool) (bool, error) {
		return false, prompt.ErrNonInteractive
	})
	got := p.Confirm("Continue? ")
	if got != DecisionYes {
		t.Errorf("expected DecisionYes from raw 'y', got %v", got)
	}
}

func TestStdinPrompter_ConfirmSuccess_returnsDecisionYes(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return true, nil
	})
	got := p.Confirm("Proceed?")
	if got != DecisionYes {
		t.Errorf("expected DecisionYes, got %v", got)
	}
}

func TestStdinPrompter_ConfirmFalse_returnsDecisionNo(t *testing.T) {
	p := newTestPrompter("", func(_, _ string, _ bool) (bool, error) {
		return false, nil
	})
	got := p.Confirm("Proceed?")
	if got != DecisionNo {
		t.Errorf("expected DecisionNo, got %v", got)
	}
}
