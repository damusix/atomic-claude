package doctor

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/prompt"
)

// stdinPrompter reads decisions from r and writes prompts to w.
// When stdin and stdout are real TTYs it delegates to the shared
// internal/prompt package (charmbracelet/huh) for richer UI.
// When running non-interactively (piped input, tests) it falls back
// to the raw line-based parser so existing behaviour is preserved.
type stdinPrompter struct {
	scanner *bufio.Scanner
	out     io.Writer
}

// NewStdinPrompter constructs a Prompter that reads from r and writes to w.
// Production callers pass os.Stdin and os.Stdout.
func NewStdinPrompter(r io.Reader, w io.Writer) Prompter {
	return &stdinPrompter{
		scanner: bufio.NewScanner(r),
		out:     w,
	}
}

func (p *stdinPrompter) Confirm(promptText string) Decision {
	// Try the huh-backed prompt when we have a real TTY.
	result, err := prompt.Confirm(promptText, "", false)
	if err == nil {
		if result {
			return DecisionYes
		}
		return DecisionNo
	}
	// ErrAborted (huh Ctrl+C) → stop the entire repair loop.
	if errors.Is(err, prompt.ErrAborted) {
		return DecisionAbort
	}
	// ErrNonInteractive → fall back to raw line parsing.
	// Any other error from huh → also fall back.
	if !errors.Is(err, prompt.ErrNonInteractive) {
		// Unexpected huh error: treat as No.
		fmt.Fprintf(p.out, "prompt error: %v\n", err)
		return DecisionNo
	}

	// Raw line-based fallback (used in tests and piped input).
	fmt.Fprint(p.out, promptText)
	if !p.scanner.Scan() {
		return DecisionNo // EOF or error → default No
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
		// "", "n", "N", or anything unrecognized → No
		return DecisionNo
	}
}

func (p *stdinPrompter) Indexed(items []string) int {
	// Try huh-backed select when we have a real TTY.
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
	// ErrNonInteractive or any other error → fall back to raw input.
	if !errors.Is(err, prompt.ErrNonInteractive) {
		fmt.Fprintf(p.out, "select error: %v\n", err)
		return 0
	}

	// Raw line-based fallback.
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
