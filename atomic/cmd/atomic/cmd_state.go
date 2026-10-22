package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/spf13/cobra"
)

func buildStateCmd(repoOverride *string) *cobra.Command {
	parent := &cobra.Command{
		Use:   "state",
		Short: "Manage the harness-neutral repository-state selection (adopt)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { runState(args, *repoOverride); return nil },
	}
	adopt := &cobra.Command{
		Use:                "adopt",
		Short:              "Select this repository's state root",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runState(append([]string{"adopt"}, args...), *repoOverride)
			return nil
		},
	}
	adopt.Flags().String("dir", "", "state directory segment or absolute path to select")
	adopt.Flags().Bool("clear", false, "drop the persisted selection so the neutral ladder decides again")
	adopt.Flags().Bool("dry-run", false, "print what would happen; make no changes")
	adopt.Flags().Bool("json", false, "emit machine-readable JSON output")
	parent.AddCommand(adopt)
	return parent
}

// stateReport is what `state adopt` prints: the resolved selection, the
// candidates discovery found, and the requested action.
type stateReport struct {
	Action     string                  `json:"action"`
	RepoRoot   string                  `json:"repo_root"`
	StateDir   string                  `json:"state_dir,omitempty"`
	Root       string                  `json:"root,omitempty"`
	Source     config.StateSource      `json:"source,omitempty"`
	Candidates []config.StateCandidate `json:"candidates,omitempty"`
}

func runState(args []string, repoOverride string) {
	if len(args) == 0 || args[0] != "adopt" {
		fmt.Fprintln(os.Stderr, "Usage: atomic state adopt [--dir <segment|path>] [--clear] [--dry-run] [--json]")
		os.Exit(2)
	}

	fs := flag.NewFlagSet("state adopt", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic state adopt [--dir <segment|path>] [--clear] [--dry-run] [--json]")
	var dir string
	var clear, dryRun, jsonOut bool
	fs.StringVar(&dir, "dir", "", "state directory segment or absolute path to select")
	fs.BoolVar(&clear, "clear", false, "drop the persisted selection")
	fs.BoolVar(&dryRun, "dry-run", false, "print what would happen; make no changes")
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	if err := fs.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if clear && dir != "" {
		fmt.Fprintln(os.Stderr, "atomic state adopt: --clear and --dir are mutually exclusive")
		os.Exit(2)
	}

	repoRoot, err := stateRepoRoot(repoOverride, fs.Args())
	if err != nil {
		fatal("state adopt", err)
	}
	report, err := stateAdopt(stateOptions{RepoRoot: repoRoot, Dir: dir, Clear: clear, DryRun: dryRun})
	if err != nil {
		fatal("state adopt", err)
	}

	if jsonOut {
		encodeJSON(report)
		return
	}
	printStateReport(report, repoRoot, dryRun)
}

// stateOptions is the parsed intent of one `state adopt` invocation.
type stateOptions struct {
	RepoRoot string
	Dir      string
	Clear    bool
	DryRun   bool
}

// stateAdopt performs the selection and returns what it did, so a test reaches
// every branch without a subprocess or an exit.
func stateAdopt(opts stateOptions) (stateReport, error) {
	// Resolving first surfaces the retired-harness refusal before anything is
	// written, so an obsolete environment never silently selects a root.
	resolved, err := config.ResolveStateLocation(opts.RepoRoot)
	if err != nil {
		return stateReport{}, err
	}
	candidates, err := config.DiscoverStateCandidates(opts.RepoRoot)
	if err != nil {
		return stateReport{}, err
	}

	report := stateReport{RepoRoot: opts.RepoRoot, Candidates: candidates}
	switch {
	case opts.Clear:
		report.Action = "clear"
		if !opts.DryRun {
			data, readErr := os.ReadFile(config.StateLocationRecordPath(opts.RepoRoot))
			if readErr != nil && !os.IsNotExist(readErr) {
				return report, readErr
			}
			if readErr == nil {
				if err := config.WithdrawStateSelection(opts.RepoRoot, data); err != nil {
					return report, err
				}
			}
		}
		report.Source = resolved.Source
		report.Root = resolved.Root
	case opts.Dir != "":
		report.Action = "select"
		report.StateDir = opts.Dir
		if !opts.DryRun {
			if _, err := config.PersistStateSelection(opts.RepoRoot, opts.Dir); err != nil {
				return report, err
			}
			report.Source = config.StateSourceRecord
		}
	default:
		switch len(candidates) {
		case 0:
			report.Action = "none"
			report.Source = resolved.Source
			report.Root = resolved.Root
		case 1:
			report.Action = "adopt"
			report.StateDir = candidates[0].StateDir
			report.Root = candidates[0].Root
			if !opts.DryRun {
				if _, err := config.PersistStateSelection(opts.RepoRoot, candidates[0].StateDir); err != nil {
					return report, err
				}
				report.Source = config.StateSourceRecord
			}
		default:
			return report, fmt.Errorf("more than one populated state root; re-run with --dir <segment|path>")
		}
	}
	return report, nil
}

// printStateReport renders one selection outcome. The selected directory is
// printed as the absolute path resolution would use: an absolute --dir is
// already resolved, a segment joins the repository root.
func printStateReport(report stateReport, repoRoot string, dryRun bool) {
	switch report.Action {
	case "clear":
		fmt.Printf("cleared\t%s\n", config.StateLocationRecordPath(repoRoot))
	case "select", "adopt":
		root := report.StateDir
		if !filepath.IsAbs(root) {
			root = filepath.Join(repoRoot, root)
		}
		fmt.Printf("%s\t%s\n", report.Action, root)
	default:
		fmt.Printf("resolution\t%s\t%s\n", report.Root, report.Source)
	}
	if dryRun {
		fmt.Println("(dry-run — no changes written)")
	}
}

// stateRepoRoot resolves the repository root the selection is recorded for: an
// explicit positional root, the --repo override, or the invoked directory's
// repo root. It uses the same resolver every repo-local verb uses, so a
// subdirectory invocation records the selection for the repository rather than
// for the subdirectory.
func stateRepoRoot(repoOverride string, rest []string) (string, error) {
	if len(rest) > 0 && rest[0] != "" {
		return repoctx.Resolve(rest[0])
	}
	return repoctx.Resolve(repoOverride)
}
