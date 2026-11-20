package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/damusix/atomic-claude/atomic/internal/workflow"
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
	c.Flags().String("harness", "", workflowHarnessHelp)
	return c
}

// runWhere reports cwd across three independent axes — repo-scope wiki
// presence, realm position, code-index scope — descriptively, with no severity.
func runWhere(args []string, repoOverride string) {
	fs := flag.NewFlagSet("where", flag.ContinueOnError)
	cliutil.SetUsage(fs, "atomic where [--json] [--harness claude|omp|codex]")
	var jsonOut bool
	var harnessFlag string
	fs.BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	fs.StringVar(&harnessFlag, "harness", "", workflowHarnessHelp)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}
	chosen, err := harness.Choose(harnessFlag, func() harness.Detection {
		return harness.Detect(harness.Environ())
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}

	cwd, err := scopeCommandDir(repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: get home dir: %v\n", err)
		os.Exit(2)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	root, insideLink, err := localRealmFromLink(cwd, claudeMDPath)
	if err == nil {
		switch {
		case root != "":
			cwd = root
		case insideLink:
			cwd, err = filepath.EvalSymlinks(cwd)
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}

	report, err := where.Resolve(cwd, claudeMDPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic where: %v\n", err)
		os.Exit(2)
	}

	if jsonOut {
		data, jerr := whereJSON(report, cwd, home, string(chosen.Kind))
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "atomic where: %v\n", jerr)
			os.Exit(2)
		}
		fmt.Println(data)
		os.Exit(0)
	}

	fmt.Print(where.FormatHuman(report))
	os.Exit(0)
}

func scopeCommandDir(repoOverride string) (string, error) {
	if repoOverride != "" && filepath.IsAbs(repoOverride) {
		return scopeRepoDir(repoOverride, repoOverride)
	}
	// On Unix Getwd uses PWD only if it names the actual working directory.
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	if filepath.Clean(cwd) != cwd {
		cwd, err = filepath.EvalSymlinks(cwd)
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if repoOverride == "" {
		return cwd, nil
	}
	if filepath.Clean(repoOverride) == repoOverride && filepath.IsLocal(repoOverride) {
		return scopeRepoDir(filepath.Join(cwd, repoOverride), repoOverride)
	}
	return scopeRepoDir(cwd+string(filepath.Separator)+repoOverride, repoOverride)
}

func scopeRepoDir(dir, repoOverride string) (string, error) {
	if filepath.Clean(dir) != dir {
		physical, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return "", fmt.Errorf("--repo %q: %w", repoOverride, err)
		}
		dir = physical
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("--repo %q is not a directory", repoOverride)
	}
	return dir, nil
}

func localRealmFromLink(cwd, claudeMDPath string) (string, bool, error) {
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return "", false, nil
	}
	var links []string
	for dir := cwd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
			links = append(links, dir)
		}
	}
	for i, link := range slices.Backward(links) {
		loc, found, err := wiki.FindRealm(filepath.Dir(link), claudeMDPath)
		if err != nil {
			return "", false, err
		}
		if !found {
			continue
		}
		roots, err := realmshare.LinkedRoots(loc.Root)
		if os.IsNotExist(err) {
			return "", true, nil
		}
		if err != nil {
			return "", true, err
		}
		if !slices.Contains(roots, link) {
			return "", true, nil
		}
		if i > 0 {
			target, err := filepath.EvalSymlinks(link)
			if err != nil {
				return "", true, err
			}
			physical, err := filepath.EvalSymlinks(cwd)
			if err != nil {
				return "", true, err
			}
			rel, err := filepath.Rel(target, physical)
			if err != nil || !filepath.IsLocal(rel) {
				return "", true, nil
			}
		}
		return loc.Root, true, nil
	}
	return "", false, nil
}

// whereJSON extends where.FormatJSON's output with project-keyed state paths
// and the current branch — additive fields only, so existing consumers of
// `atomic where --json` (wiki/realm/code-index scope checks) are unaffected.
//
// Branch and reports paths depend on report.RepoRoot.Path actually holding a
// `.git` entry (report.RepoRoot itself may be a scope="repo" marker directory
// with none); when config.BranchFromHEAD finds no `.git`, "branch" and
// "reports" are omitted rather than guessed. reports_root and archive
// resolve unconditionally since they name a project home, not a branch.
func whereJSON(report where.Report, cwd, home, harnessKind string) (string, error) {
	base, err := where.FormatJSON(report)
	if err != nil {
		return "", err
	}

	var obj map[string]any
	if err := json.Unmarshal([]byte(base), &obj); err != nil {
		return "", fmt.Errorf("unmarshal base where json: %w", err)
	}

	repoRoot := report.RepoRoot.Path
	stateRoot, err := config.RepositoryStateDir(repoRoot)
	if err != nil {
		return "", err
	}
	if branch, ok := config.BranchFromHEAD(repoRoot); ok {
		obj["branch"] = branch
		obj["reports"] = config.ReportsDir(repoRoot, branch)
	}
	obj["reports_root"] = config.ReportsRoot(repoRoot)
	obj["archive"] = scratchpad.ArchiveRoot(repoRoot)

	obj["state_root"] = stateRoot
	worktrees := workflow.Worktrees(repoRoot, cwd, home, harnessKind)
	if worktrees == "off" {
		worktrees = ""
	}
	obj["worktrees"] = worktrees

	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal extended where json: %w", err)
	}
	return string(data), nil
}
