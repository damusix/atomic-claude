// Package validate implements `atomic validate`: deterministic artifact linting
// with exit codes 0 (pass or warn), 1 (fail), 2 (validator error).
package validate

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Run reports an exit code: 0 ok, 1 FAIL findings, 2 validator error.
func Run(args []string) int {
	return RunWithOutput(args, os.Stdout)
}

// RunWithOutput is Run with usage and help redirected to w, so tests avoid
// exec.Command round-trips.
func RunWithOutput(args []string, w io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(w)

	var jsonOut bool
	var suggest bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON output ({schema_version:1, findings:[...]})")
	fs.BoolVar(&suggest, "suggest", false, "print structural templates for content-FAIL rules")

	fs.Usage = func() {
		fmt.Fprintf(w, "Usage: atomic validate [flags] [spec|config|bundle|artifacts] [paths...]\n\n")
		fmt.Fprintf(w, "Subcommands:\n")
		fmt.Fprintf(w, "  spec       [paths...]  Validate spec structure (S0,S1,S5,S6)\n")
		fmt.Fprintf(w, "  config     [paths...]  Validate cross-reference integrity (C3,C5,C7,C9)\n")
		fmt.Fprintf(w, "  bundle                 Validate bundle parity vs committed embedded/\n")
		fmt.Fprintf(w, "  artifacts  [paths...]  Lint atomic CLI verb/flag citations in artifacts\n")
		fmt.Fprintf(w, "\nFlags:\n")
		fmt.Fprintln(w, "  --json     emit JSON output ({schema_version:1, findings:[...]})")
		fmt.Fprintln(w, "  --suggest  print structural templates for content-FAIL rules")
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	remaining := fs.Args()

	if len(remaining) == 0 {
		return runWholeRepo(jsonOut, suggest, w)
	}

	sub := remaining[0]
	subArgs := remaining[1:]

	// A first arg that looks like a path, not a bare verb, makes every
	// remaining arg a path.
	if isPathArg(sub) {
		return runPathDispatch(append([]string{sub}, subArgs...), jsonOut, suggest, w)
	}

	switch sub {
	case "spec":
		return runSpec(subArgs, jsonOut, suggest, w)
	case "config":
		return runConfig(subArgs, jsonOut, suggest, w)
	case "bundle":
		return runBundle(subArgs, jsonOut, suggest, w)
	case "artifacts":
		paths, jOut, sug, ok := parseArtifactsFlags(subArgs, w)
		if !ok {
			return 2
		}
		return runArtifacts(paths, jOut || jsonOut, sug || suggest, w)
	default:
		fmt.Fprintf(w, "atomic validate: unknown subcommand %q\n", sub)
		fmt.Fprintf(w, "  subcommands: spec, config, bundle, artifacts\n")
		return 2
	}
}

func runBundle(paths []string, jsonOut, suggest bool, w io.Writer) int {
	_ = paths
	return runBundleImpl(jsonOut, suggest, w)
}
