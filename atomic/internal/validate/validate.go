// Package validate implements the `atomic validate` subcommand: deterministic,
// fast artifact linting with exit codes 0 (pass/warn), 1 (fail), 2 (error).
// CP-1 scaffolds dispatch and flag parsing only; rule logic ships in later
// checkpoints (CP-5 spec rules, CP-6 config rules, CP-3 bundle parity).
package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Run is the entry point called from main. Returns an exit code: 0 ok, 1 FAIL
// findings, 2 validator-internal error (bad invocation, unreadable file, etc.).
func Run(args []string) int {
	return RunWithOutput(args, os.Stdout)
}

// RunWithOutput is like Run but writes usage/help to w. Extracted so tests can
// capture output without exec.Command round-trips.
func RunWithOutput(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(w)

	var jsonOut bool
	var suggest bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output ({schema_version:1, findings:[...]})")
	fs.BoolVar(&suggest, "suggest", false, "print structural templates for content-FAIL rules")

	fs.Usage = func() {
		fmt.Fprintf(w, "Usage: atomic validate [flags] [spec|config|bundle] [paths...]\n\n")
		fmt.Fprintf(w, "Subcommands:\n")
		fmt.Fprintf(w, "  spec    [paths...]  Validate spec structure (S0,S1,S5,S6)\n")
		fmt.Fprintf(w, "  config  [paths...]  Validate cross-reference integrity (C1,C3,C5,C7,C9)\n")
		fmt.Fprintf(w, "  bundle              Validate bundle parity vs committed embedded/\n")
		fmt.Fprintf(w, "\nFlags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		// flag.ContinueOnError: -h/-help causes ErrHelp, already printed usage.
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	remaining := fs.Args()

	// No subcommand: run all validators (whole-repo mode, CP-8).
	// For CP-1 stub this falls through to "no subcommand yet" path.
	if len(remaining) == 0 {
		// Whole-repo mode stub — wired in CP-8.
		fmt.Fprintf(w, "atomic validate: no subcommand specified; whole-repo mode not yet implemented\n")
		fmt.Fprintf(w, "  subcommands: spec, config, bundle\n")
		return 2
	}

	sub := remaining[0]
	subArgs := remaining[1:]

	_ = jsonOut
	_ = suggest

	switch sub {
	case "spec":
		return runSpec(subArgs, jsonOut, suggest, w)
	case "config":
		return runConfig(subArgs, jsonOut, suggest, w)
	case "bundle":
		return runBundle(subArgs, jsonOut, suggest, w)
	default:
		fmt.Fprintf(w, "atomic validate: unknown subcommand %q\n", sub)
		fmt.Fprintf(w, "  subcommands: spec, config, bundle\n")
		return 2
	}
}

// runSpec is the spec validator entry point. Stub in CP-1; rules ship in CP-5.
func runSpec(paths []string, jsonOut, suggest bool, w io.Writer) int {
	_ = paths
	_ = jsonOut
	_ = suggest
	fmt.Fprintf(w, "atomic validate spec — stub (rules ship in CP-5)\n")
	return 0
}

// runConfig is the config validator entry point. Stub in CP-1; rules ship in CP-6.
func runConfig(paths []string, jsonOut, suggest bool, w io.Writer) int {
	_ = paths
	_ = jsonOut
	_ = suggest
	fmt.Fprintf(w, "atomic validate config — stub (rules ship in CP-6)\n")
	return 0
}

// runBundle is the bundle validator entry point. Wired in CP-3.
func runBundle(paths []string, jsonOut, suggest bool, w io.Writer) int {
	_ = paths
	_ = jsonOut
	_ = suggest
	return runBundleImpl(w)
}
