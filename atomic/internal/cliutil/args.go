package cliutil

import (
	"flag"
	"fmt"
)

// ParseInterspersed parses flags in any position and returns the positionals in
// order; everything after the first bare "--" is positional. Errors pass through.
func ParseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	head := args
	var tail []string
	for i, a := range args {
		if a == "--" {
			head = args[:i]
			tail = args[i+1:]
			break
		}
	}

	// fs.Parse stops at the first positional, so peel one off and re-parse the rest.
	var positional []string
	rest := head
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	return append(positional, tail...), nil
}

// UnexpectedArgs reports stray positionals for cmd on fs.Output(), prints the
// usage, and returns the error so the caller picks its own exit code.
func UnexpectedArgs(fs *flag.FlagSet, cmd string, extra []string) error {
	err := fmt.Errorf("unexpected argument(s): %q", extra)
	fmt.Fprintf(fs.Output(), "atomic %s: %v\n", cmd, err)
	fs.Usage()
	return err
}
