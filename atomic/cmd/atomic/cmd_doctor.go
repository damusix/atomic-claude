package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

// buildDoctorCmd returns the "doctor" top-level command with flag metadata.
func buildDoctorCmd() *cobra.Command {
	c := &cobra.Command{
		Use:                "doctor",
		Short:              "Integrity check",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runDoctor(args)
			return nil
		},
	}
	// Metadata only — not parsed at runtime (DisableFlagParsing:true).
	c.Flags().Bool("fix", false, "per-item confirm prompt before applying any repair")
	c.Flags().Bool("json", false, "emit machine-readable JSON result to stdout")
	c.Flags().String("only", "", "comma-separated category indices or names to run")
	c.Flags().String("skip", "", "comma-separated category indices or names to skip")
	c.Flags().Int("stale-days", 7, "stale-signals threshold in days (positive int)")
	c.Flags().Bool("verbose", false, "print per-file detail for install integrity")
	return c
}

func runDoctor(args []string) {
	opts, err := doctor.ParseFlags(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}

	// Resolve home directory for the missing-~/.claude/ short-circuit.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic doctor: resolve home dir: %v\n", err)
		os.Exit(2)
	}

	if doctor.ClaudeHomeMissing(home) {
		msg := doctor.MissingHomeMessage()
		if opts.JSON {
			data, jerr := doctor.FormatJSONMissingHome(msg)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "atomic doctor: marshal json: %v\n", jerr)
				os.Exit(2)
			}
			fmt.Println(string(data))
		} else {
			fmt.Println(msg)
		}
		os.Exit(0)
	}

	// Resolve project name: git toplevel basename, or cwd basename on failure.
	project := doctorProjectName()

	// Wire claudeMDPath for realm detection in check 11 (code-index).
	opts.ClaudeMDPath = filepath.Join(home, ".claude", "CLAUDE.md")

	results, err := doctor.Run(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic doctor: %v\n", err)
		os.Exit(2)
	}

	exitCode := doctor.ExitCode(results)

	if opts.JSON {
		data, jerr := doctor.FormatJSON(results, project, exitCode)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "atomic doctor: marshal json: %v\n", jerr)
			os.Exit(2)
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(doctor.FormatHuman(results, opts, project))
	}

	if opts.Fix {
		p := doctor.NewStdinPrompter(os.Stdin, os.Stdout)
		summary := doctor.Repair(results, opts, p, os.Stdout)
		exitCode = postRepairExitCode(exitCode, summary.Applied, func() ([]doctor.Result, error) {
			return doctor.Run(opts)
		})
	}

	os.Exit(exitCode)
}

// postRepairExitCode returns the status `atomic doctor --fix` should terminate
// with once the repair pass has run.
//
// Repairs mutate the very state the checks examined, so the pre-repair verdict
// is stale by the time the process exits. A caller gating CI on `--fix` has to
// be able to tell "found problems and fixed them" from "still broken", and the
// pre-repair code collapses both to 1.
//
// Re-checking costs a second full pass, so it only runs when a repair actually
// landed. A re-check that itself errors keeps the original verdict rather than
// reporting success for a state nobody managed to observe.
func postRepairExitCode(pre, applied int, recheck func() ([]doctor.Result, error)) int {
	if applied <= 0 {
		return pre
	}
	after, err := recheck()
	if err != nil {
		return pre
	}
	return doctor.ExitCode(after)
}

// doctorProjectName returns the project name to display in doctor output.
// Uses the git toplevel directory basename; falls back to cwd basename.
func doctorProjectName() string {
	out, err := repoctx.Resolve("")
	if err == nil && out != "" {
		return filepath.Base(out)
	}
	cwd, err := os.Getwd()
	if err == nil {
		return filepath.Base(cwd)
	}
	return "unknown"
}
