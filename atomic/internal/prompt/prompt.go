// Package prompt wraps charmbracelet/huh to provide interactive prompts with
// TTY detection and a testable internal seam.
package prompt

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	charmterm "github.com/charmbracelet/x/term"
)

// ErrNonInteractive is returned when stdin or stdout is not a TTY and the
// caller should fall back to a non-interactive default.
var ErrNonInteractive = errors.New("non-interactive terminal")

// ErrAborted is returned when the user force-quits the TUI (Ctrl+C /
// huh.ErrUserAborted). Distinct from ErrNonInteractive so callers can
// differentiate "no TTY" from "user intentionally aborted".
var ErrAborted = errors.New("user aborted")

// Option is a selectable item for Select.
type Option[T comparable] struct {
	Label       string
	Value       T
	Description string
}

// isInteractive returns true when both stdin and stdout are TTYs.
func isInteractive() bool {
	return charmterm.IsTerminal(os.Stdin.Fd()) &&
		charmterm.IsTerminal(os.Stdout.Fd())
}

// runConfirm is the internal seam; tests replace it to avoid spawning a TTY.
var runConfirm = defaultRunConfirm

// runSelect is the internal seam for tests. Because package-level vars cannot
// be generic in Go 1.23, we store it as interface{} and type-assert inside
// Select[T]. Tests set it via withSelectStub with a concrete closure; nil
// means: use defaultRunSelect.
var runSelect interface{} = nil

func defaultRunConfirm(title, desc string, def bool) (bool, error) {
	if !isInteractive() {
		return false, ErrNonInteractive
	}
	var result bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(desc).
				Value(&result),
		),
	)
	// Initialise result to the default value before running the form.
	result = def
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, ErrAborted
		}
		return false, err
	}
	return result, nil
}

// Confirm presents a yes/no prompt and returns the user's choice.
// Returns (false, ErrNonInteractive) when stdin/stdout are not TTYs.
func Confirm(title, desc string, def bool) (bool, error) {
	return runConfirm(title, desc, def)
}

// defaultRunSelect[T] is the real huh-backed implementation.
func defaultRunSelect[T comparable](title string, opts []Option[T]) (T, error) {
	var zero T
	if !isInteractive() {
		return zero, ErrNonInteractive
	}
	huhOpts := make([]huh.Option[T], len(opts))
	for i, o := range opts {
		huhOpts[i] = huh.NewOption(o.Label, o.Value)
	}
	var result T
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[T]().
				Title(title).
				Options(huhOpts...).
				Value(&result),
		),
	)
	if err := form.Run(); err != nil {
		return zero, err
	}
	return result, nil
}

// Select presents a single-pick list and returns the chosen value.
// Returns (zero, ErrNonInteractive) when stdin/stdout are not TTYs.
func Select[T comparable](title string, opts []Option[T]) (T, error) {
	var zero T
	if len(opts) == 0 {
		return zero, fmt.Errorf("prompt.Select: no options provided")
	}
	// If a test has injected a stub for this T, use it.
	if runSelect != nil {
		if fn, ok := runSelect.(func(string, []Option[T]) (T, error)); ok {
			return fn(title, opts)
		}
	}
	return defaultRunSelect(title, opts)
}
