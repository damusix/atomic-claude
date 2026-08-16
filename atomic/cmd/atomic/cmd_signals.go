package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/signals"
	"github.com/spf13/cobra"
)

// buildSignalsCmd builds the "signals" parent + scan|show|stale|diff|linkify children.
func buildSignalsCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runSignals(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "signals",
		Short: "Project context pipeline (scan|show|stale|diff|linkify)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	addSub := func(verb, short, argsHint string, flagFn func(*cobra.Command)) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": argsHint},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				dispatch(append([]string{verb}, args...))
				return nil
			},
		}
		if flagFn != nil {
			flagFn(c)
		}
		parent.AddCommand(c)
	}
	addSub("scan", "Walk repo and write docs/wiki/scan.md", "", func(c *cobra.Command) {
		c.Flags().String("out", "", "write substrate to <dir> instead of <root>/.claude/project/")
	})
	addSub("show", "Print docs/wiki/scan.md to stdout", "", nil)
	addSub("stale", "Exit 0 fresh, 1 stale, 2 error", "", nil)
	addSub("diff", "Print unified diff of signals file", "", nil)
	addSub("linkify", "Linkify path tokens in docs/wiki/index.md and docs/wiki/*.md", "", nil)
	return parent
}

func runSignals(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic signals <scan|show|stale|diff>\n")
		os.Exit(1)
	}

	root, err := repoctx.Resolve(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic signals: %v\n", err)
		os.Exit(1)
	}

	verb := args[0]
	switch verb {
	case "scan":
		fs := flag.NewFlagSet("signals-scan", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		var outDir string
		fs.StringVar(&outDir, "out", "", "write substrate to <dir> instead of <root>/.claude/project/")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		opts := &signals.Options{}
		if outDir != "" {
			absOut, err := filepath.Abs(outDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "atomic signals scan: resolve --out: %v\n", err)
				os.Exit(1)
			}
			opts.OutDir = absOut
		}
		if err := signals.ScanWithOptions(root, opts); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := signals.Show(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	case "stale":
		info, err := signals.Stale(root)
		if err == nil {
			return // fresh → exit 0, silent
		}
		if err == signals.ErrStale {
			// Imperative, evidence-bearing output. The staleness gate is read by
			// an LLM orchestrator that can rationalize a silent exit code away, so
			// the tool states the directive and the evidence, not just the state.
			// Deliberate model-safeguard layer over the deterministic exit code —
			// see the prefer-code-over-model exception in CLAUDE.md.
			fmt.Printf("signals: STALE — a fresh scan would change the deterministic snapshot (~%d lines)\n", info.ChangedLines)
			fmt.Printf("→ refresh required; dispatch atomic-wiki-inferrer. do not skip.\n")
			os.Exit(1)
		}
		// Hard error (e.g. missing signals file): exit 2, distinct from the
		// exit-1 stale signal so callers can tell "out of date" from "broken".
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	case "diff":
		err := signals.Diff(root, os.Stdout)
		if err == nil {
			return // no diff → exit 0
		}
		if err == signals.ErrDiffPresent {
			os.Exit(1)
		}
		if err == signals.ErrNoPrior {
			os.Exit(2)
		}
		// Hard error: exit 2, alongside ErrNoPrior — distinct from the exit-1
		// "diff present" signal. See the check-family exit convention.
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	case "linkify":
		fs := flag.NewFlagSet("signals-linkify", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		// Root follows the signals convention (cwd / global --repo), like
		// scan and stale. There is no per-verb --root flag here.
		if err := signals.LinkifyFiles(root); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "atomic signals: unknown verb %q\n", verb)
		os.Exit(1)
	}
}
