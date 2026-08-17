package prompt

import (
	"errors"
	"testing"
)

func withConfirmStub(result bool, err error, f func()) {
	orig := runConfirm
	runConfirm = func(_ string, _ string, _ bool) (bool, error) { return result, err }
	defer func() { runConfirm = orig }()
	f()
}

func withSelectStub[T comparable](result T, err error, f func()) {
	orig := runSelect
	runSelect = func(_ string, _ []Option[T]) (T, error) { return result, err }
	defer func() { runSelect = orig }()
	f()
}

func TestConfirm_returnsResultFromRunner(t *testing.T) {
	withConfirmStub(true, nil, func() {
		got, err := Confirm("title", "desc", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("expected true, got false")
		}
	})
}

func TestConfirm_defaultFalseReturnsFalse(t *testing.T) {
	withConfirmStub(false, nil, func() {
		got, err := Confirm("title", "desc", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("expected false, got true")
		}
	})
}

func TestConfirm_propagatesRunnerError(t *testing.T) {
	sentinel := errors.New("runner error")
	withConfirmStub(false, sentinel, func() {
		_, err := Confirm("title", "desc", false)
		if !errors.Is(err, sentinel) {
			t.Errorf("expected sentinel error, got %v", err)
		}
	})
}

func TestConfirm_nonInteractive_returnsErrNonInteractive(t *testing.T) {
	withConfirmStub(false, ErrNonInteractive, func() {
		_, err := Confirm("title", "desc", false)
		if !errors.Is(err, ErrNonInteractive) {
			t.Errorf("expected ErrNonInteractive, got %v", err)
		}
	})
}

func TestSelect_returnsChosenValue(t *testing.T) {
	opts := []Option[string]{
		{Label: "A", Value: "a"},
		{Label: "B", Value: "b"},
	}
	withSelectStub("b", nil, func() {
		got, err := Select("pick", opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "b" {
			t.Errorf("expected 'b', got %q", got)
		}
	})
}

func TestSelect_propagatesRunnerError(t *testing.T) {
	opts := []Option[int]{{Label: "one", Value: 1}}
	sentinel := errors.New("select error")
	withSelectStub(0, sentinel, func() {
		_, err := Select("pick", opts)
		if !errors.Is(err, sentinel) {
			t.Errorf("expected sentinel error, got %v", err)
		}
	})
}

func TestSelect_nonInteractive_returnsErrNonInteractive(t *testing.T) {
	opts := []Option[int]{{Label: "one", Value: 1}}
	withSelectStub(0, ErrNonInteractive, func() {
		_, err := Select("pick", opts)
		if !errors.Is(err, ErrNonInteractive) {
			t.Errorf("expected ErrNonInteractive, got %v", err)
		}
	})
}

func TestSelect_emptyOptions_returnsError(t *testing.T) {
	var opts []Option[string]
	got, err := Select("pick", opts)
	if err == nil {
		t.Error("expected error for empty options, got nil")
	}
	if got != "" {
		t.Errorf("expected zero value, got %q", got)
	}
}

func TestConfirm_abortedRunner_returnsErrAborted(t *testing.T) {
	// Callers must be able to tell Ctrl+C from N; folding abort into No would
	// silently swallow a force-quit.
	withConfirmStub(false, ErrAborted, func() {
		_, err := Confirm("title", "desc", false)
		if !errors.Is(err, ErrAborted) {
			t.Errorf("expected ErrAborted, got %v", err)
		}
	})
}

func TestErrAborted_isDistinctFromErrNonInteractive(t *testing.T) {
	if ErrAborted == nil {
		t.Fatal("ErrAborted must not be nil")
	}
	if errors.Is(ErrAborted, ErrNonInteractive) {
		t.Error("ErrAborted must not match ErrNonInteractive")
	}
}

func TestErrNonInteractive_isDistinctError(t *testing.T) {
	if ErrNonInteractive == nil {
		t.Fatal("ErrNonInteractive must not be nil")
	}
	if errors.Is(ErrNonInteractive, errors.New("other")) {
		t.Error("ErrNonInteractive must not match arbitrary errors")
	}
}

func TestOption_fields(t *testing.T) {
	o := Option[int]{Label: "one", Value: 1, Description: "the first"}
	if o.Label != "one" || o.Value != 1 || o.Description != "the first" {
		t.Errorf("unexpected option fields: %+v", o)
	}
}
