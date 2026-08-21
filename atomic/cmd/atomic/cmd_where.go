package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/spf13/cobra"
)

func buildWhereCmd(repoOverride *string) *cobra.Command {
	c := &cobra.Command{
		Use:                "where",
		Short:              "Report cwd's wiki/realm/code-index position",
		Annotations:        map[string]string{"args_hint": ""},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runWhere(args, *repoOverride)
			return nil
		},
	}
	c.Flags().Bool("json", false, "emit machine-readable JSON output")
	return c
}

// runWhere reports cwd across three independent axes — repo-scope wiki
// presence, realm position, code-index scope — descriptively, with no severity.
func runWhere(args []string, repoOverride string) {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic where [--json]")
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: get cwd: %v\n", err)
		os.Exit(2)
	}
	// --repo relocates the whole report, not only the repo_root line: branch,
	// reports, and reminders are all derived from wherever resolution starts.
	if repoOverride != "" {
		abs, aerr := filepath.Abs(repoOverride)
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "atomic where: --repo %q: %v\n", repoOverride, aerr)
			os.Exit(2)
		}
		if st, serr := os.Stat(abs); serr != nil || !st.IsDir() {
			fmt.Fprintf(os.Stderr, "atomic where: --repo %q is not a directory\n", repoOverride)
			os.Exit(2)
		}
		cwd = abs
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: get home dir: %v\n", err)
		os.Exit(2)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")

	report, err := where.Resolve(cwd, claudeMDPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}

	if jsonOut {
		data, jerr := whereJSON(report)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "atomic where: marshal json: %v\n", jerr)
			os.Exit(2)
		}
		fmt.Println(data)
		os.Exit(0)
	}

	fmt.Print(where.FormatHuman(report))
	os.Exit(0)
}

// whereJSON extends where.FormatJSON's output with project-keyed state paths
// and the current branch — additive fields only, so existing consumers of
// `atomic where --json` (wiki/realm/code-index scope checks) are unaffected.
//
// Branch and reports paths depend on report.RepoRoot.Path actually holding a
// `.git` entry (report.RepoRoot itself may be a scope="repo" marker directory
// with none); when config.BranchFromHEAD finds no `.git`, "branch" and
// "reports" are omitted rather than guessed. reports_root/reminders/archive
// resolve unconditionally since they name a project home, not a branch.
func whereJSON(report where.Report) (string, error) {
	base, err := where.FormatJSON(report)
	if err != nil {
		return "", err
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(base), &obj); err != nil {
		return "", fmt.Errorf("unmarshal base where json: %w", err)
	}

	repoRoot := report.RepoRoot.Path
	if branch, ok := config.BranchFromHEAD(repoRoot); ok {
		obj["branch"] = branch
		obj["reports"] = config.ReportsDir(repoRoot, branch)
	}
	obj["reports_root"] = config.ReportsRoot(repoRoot)
	obj["reminders"] = config.ProjectRemindersDir(repoRoot)
	obj["archive"] = scratchpad.ArchiveRoot(repoRoot)

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal extended where json: %w", err)
	}
	return string(data), nil
}
