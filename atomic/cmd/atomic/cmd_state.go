package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
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
	adopt.Flags().String("dir", "", "repository-relative or absolute state directory path to select")
	adopt.Flags().Bool("clear", false, "drop the persisted selection so the neutral ladder decides again")
	adopt.Flags().Bool("dry-run", false, "print what would happen; make no changes")
	adopt.Flags().Bool("json", false, "emit machine-readable JSON output")
	migrate := &cobra.Command{
		Use:                "migrate",
		Short:              "Move repository state out of .claude into the destination root",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runState(append([]string{"migrate"}, args...), *repoOverride)
			return nil
		},
	}
	parent.AddCommand(adopt, migrate)
	return parent
}

// stateMigrate runs `atomic state migrate` for repoRoot and returns its exit
// code: 1 when the migration refuses or fails.
func stateMigrate(repoRoot string, out, errOut io.Writer) int {
	report, err := config.MigrateRepositoryState(repoRoot)
	printMigration(out, report)
	if err != nil {
		fmt.Fprintf(errOut, "atomic state migrate: %v\n", err)
		return 1
	}
	if len(report.Moved) == 0 && report.IndexRemoved == "" && report.IndexInUse == "" {
		fmt.Fprintln(out, "nothing to move")
	}
	return 0
}

// stateMigrateCmd is `atomic state migrate`. It takes no arguments, so a
// flag such as --help is never read as a repository root.
func stateMigrateCmd(args []string, repoOverride string, out, errOut io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(errOut, "Usage: atomic state migrate")
		return 2
	}
	repoRoot, err := repoctx.ResolveUnchecked(repoOverride)
	if err != nil {
		fmt.Fprintf(errOut, "atomic state migrate: %v\n", err)
		return 1
	}
	return stateMigrate(repoRoot, out, errOut)
}

func init() {
	config.IndexInUse = codeIndexInUse
}

// codeIndexInUse: a daemon answers on the socket, or a starter holds the spawn
// lock. When neither does, the lock stays held while it is removed.
func codeIndexInUse(indexDir string) (bool, func(), error) {
	db := filepath.Join(indexDir, config.IndexDBName)
	if mcp.IsLive(mcp.SocketPathFromDB(db)) {
		return true, nil, nil
	}
	release, ok, err := mcp.TryHoldSpawnLock(db)
	if err != nil || !ok {
		return true, nil, err
	}
	if mcp.IsLive(mcp.SocketPathFromDB(db)) {
		release()
		return true, nil, nil
	}
	return false, release, nil
}

// migrateStateIfPending is the `repo init` and `update` trigger.
func migrateStateIfPending(repoRoot string, notice io.Writer) error {
	pending, err := config.RepoStateMigrationPending(repoRoot)
	if errors.Is(err, config.ErrHomeStateRoot) {
		fmt.Fprintf(notice, "atomic: not migrating repository state: %v\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	// With nothing to move it still resolves the root, so a both-populated
	// repo prints its one leftover warning.
	if !pending {
		if _, err := config.RepositoryStateDir(repoRoot); err != nil {
			fmt.Fprintf(notice, "atomic: %v\n", err)
		}
		return nil
	}
	fmt.Fprintf(notice, "atomic: moving repository state out of %s (one time)\n", filepath.Join(repoRoot, ".claude"))
	report, err := config.MigrateRepositoryState(repoRoot)
	printMigration(notice, report)
	return err
}

func printMigration(w io.Writer, r config.RepoStateMigration) {
	for _, from := range r.Moved {
		fmt.Fprintf(w, "moved\t%s into %s\n", from, r.Destination)
	}
	for _, s := range r.Staged {
		fmt.Fprintf(w, "staged\t%s -> %s\n", s.From, s.To)
	}
	if r.IndexRemoved != "" {
		fmt.Fprintf(w, "removed\t%s\n", r.IndexRemoved)
	}
	if r.IndexInUse != "" {
		fmt.Fprintf(w, "kept\t%s (an MCP daemon may be live there; `atomic code index` builds a fresh index in %s)\n", r.IndexInUse, r.Destination)
	}
	for _, ref := range r.References {
		fmt.Fprintf(w, "reference\t%s names a moved .claude path; update it by hand\n", ref)
	}
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
	if len(args) > 0 && args[0] == "migrate" {
		os.Exit(stateMigrateCmd(args[1:], repoOverride, os.Stdout, os.Stderr))
	}
	if len(args) == 0 || args[0] != "adopt" {
		fmt.Fprintln(os.Stderr, "Usage: atomic state adopt [--dir <path>] [--clear] [--dry-run] [--json]")
		fmt.Fprintln(os.Stderr, "       atomic state migrate")
		os.Exit(2)
	}

	fs := flag.NewFlagSet("state adopt", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic state adopt [--dir <path>] [--clear] [--dry-run] [--json]")
	var dir string
	var clear, dryRun, jsonOut bool
	fs.StringVar(&dir, "dir", "", "repository-relative or absolute state directory path to select")
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
	// Resolving first surfaces an invalid ATOMIC_STATE_DIR before any write.
	// Ambiguity is what --dir resolves, so it does not refuse here.
	resolved, err := config.ResolveStateLocation(opts.RepoRoot)
	if err != nil && !errors.Is(err, config.ErrAmbiguousStateRoot) {
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
		chosen, ok, err := config.SelectStateCandidate(opts.RepoRoot)
		if errors.Is(err, config.ErrAmbiguousStateRoot) {
			return report, fmt.Errorf("more than one populated state root; re-run with --dir <path>")
		}
		if err != nil {
			return report, err
		}
		if !ok {
			report.Action = "none"
			report.Source = resolved.Source
			report.Root = resolved.Root
			break
		}
		report.Action = "adopt"
		report.StateDir = chosen.StateDir
		report.Root = chosen.Root
		if !opts.DryRun {
			if _, err := config.PersistStateSelection(opts.RepoRoot, chosen.StateDir); err != nil {
				return report, err
			}
			report.Source = config.StateSourceRecord
		}
	}
	return report, nil
}

// printStateReport renders one selection outcome, printing the selected directory
// as the absolute path resolution would use.
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

// stateRepoRoot resolves the repository root the selection is recorded for, so a
// subdirectory invocation records it for the repository, not the subdirectory.
func stateRepoRoot(repoOverride string, rest []string) (string, error) {
	if len(rest) > 0 && rest[0] != "" {
		return repoctx.ResolveUnchecked(rest[0])
	}
	return repoctx.ResolveUnchecked(repoOverride)
}
