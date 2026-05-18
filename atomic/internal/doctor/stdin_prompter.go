package doctor

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// stdinPrompter reads decisions from r and writes prompts to w.
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

func (p *stdinPrompter) Confirm(prompt string) Decision {
	fmt.Fprint(p.out, prompt)
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
