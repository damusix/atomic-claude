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

// ErrAborted is a deliberate Ctrl+C, kept distinct from ErrNonInteractive so a
// caller can tell "no TTY" from "user said no".
var ErrAborted = errors.New("user aborted")

type Option[T comparable] struct {
	Label       string
	Value       T
	Description string
}

func isInteractive() bool {
	return charmterm.IsTerminal(os.Stdin.Fd()) &&
		charmterm.IsTerminal(os.Stdout.Fd())
}

// runConfirm is the internal seam; tests replace it to avoid spawning a TTY.
var runConfirm = defaultRunConfirm

// runSelect is stored as interface{} and type-asserted inside Select[T]
// because a package-level var cannot be generic. nil means defaultRunSelect.
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
func Confirm(title, desc string, def bool) (bool, error) {
	return runConfirm(title, desc, def)
}

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
func Select[T comparable](title string, opts []Option[T]) (T, error) {
	var zero T
	if len(opts) == 0 {
		return zero, fmt.Errorf("prompt.Select: no options provided")
	}
	if runSelect != nil {
		if fn, ok := runSelect.(func(string, []Option[T]) (T, error)); ok {
			return fn(title, opts)
		}
	}
	return defaultRunSelect(title, opts)
}
