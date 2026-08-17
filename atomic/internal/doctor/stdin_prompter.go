package doctor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// stdinPrompter delegates to internal/prompt (huh) on a real TTY and falls
// back to raw line parsing when non-interactive (piped input, tests).
type stdinPrompter struct {
	scanner *bufio.Scanner
	out     io.Writer
	// Tests stub confirmFn to exercise ErrAborted → DecisionAbort without a TTY.
	confirmFn func(title, desc string, def bool) (bool, error)
}

// NewStdinPrompter constructs a Prompter that reads from r and writes to w.
func NewStdinPrompter(r io.Reader, w io.Writer) Prompter {
	return &stdinPrompter{
		scanner:   bufio.NewScanner(r),
		out:       w,
		confirmFn: prompt.Confirm,
	}
}

func (p *stdinPrompter) Confirm(promptText string) Decision {
	result, err := p.confirmFn(promptText, "", false)
	if err == nil {
		if result {
			return DecisionYes
		}
		return DecisionNo
	}
	// Ctrl+C stops the entire repair loop, not just this item.
	if errors.Is(err, prompt.ErrAborted) {
		return DecisionAbort
	}
	if !errors.Is(err, prompt.ErrNonInteractive) {
		fmt.Fprintf(p.out, "prompt error: %v\n", err)
		return DecisionNo
	}

	fmt.Fprint(p.out, promptText)
	if !p.scanner.Scan() {
		return DecisionNo
	}
	line := strings.TrimSpace(p.scanner.Text())
	switch strings.ToLower(line) {
	case "y":
		return DecisionYes
	case "a":
		return DecisionAll
	case "q":
		return DecisionQuit
	default:
		// Empty input and anything unrecognized default to No.
		return DecisionNo
	}
}

func (p *stdinPrompter) Indexed(items []string) int {
	opts := make([]prompt.Option[int], len(items))
	for i, name := range items {
		opts[i] = prompt.Option[int]{
			Label: fmt.Sprintf("%d. %s", i+1, name),
			Value: i + 1,
		}
	}
	opts = append(opts, prompt.Option[int]{Label: "0. cancel", Value: 0})

	val, err := prompt.Select("select a file to patch:", opts)
	if err == nil {
		return val
	}
	if !errors.Is(err, prompt.ErrNonInteractive) {
		fmt.Fprintf(p.out, "select error: %v\n", err)
		return 0
	}

	fmt.Fprintln(p.out, "select a file to patch:")
	for i, name := range items {
		fmt.Fprintf(p.out, "  %d. %s\n", i+1, name)
	}
	fmt.Fprint(p.out, "enter number (or 0 to cancel): ")
	if !p.scanner.Scan() {
		return 0
	}
	line := strings.TrimSpace(p.scanner.Text())
	var idx int
	if _, err := fmt.Sscan(line, &idx); err != nil {
		return 0
	}
	return idx
}
