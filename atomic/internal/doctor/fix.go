package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Decision is the user's per-item response in fix mode.
type Decision int

const (
	DecisionYes   Decision = iota // y — apply this repair
	DecisionNo                    // N (default) — skip this repair
	DecisionAll                   // a — apply this and all remaining
	DecisionQuit                  // q — skip this and all remaining
	DecisionAbort                 // Ctrl+C / huh ErrUserAborted — stop entire loop
)

// Prompter is the interface for interactive fix-mode prompts.
type Prompter interface {
	Confirm(prompt string) Decision
	// Indexed returns the 1-based index picked from a numbered list; 0 = none.
	Indexed(items []string) int
}

// RepairSummary is the outcome of a Repair call.
type RepairSummary struct {
	Applied    int
	Skipped    int
	NonFixable int
}

// Repairer holds the injectable repair functions used by the fix loop. Tests
// build a literal with faked fields, so there are no mutable globals to race on.
type Repairer struct {
	InstallFn         func(io.Writer) error
	HooksFn           func(io.Writer) error
	ManifestFn        func(io.Writer) error
	FollowupsRenderFn func(io.Writer) error
	HomeFn            func() (string, error)
	IsRepoDevFn       func() (bool, error)
	RepoRootFn        func() string
}

// DefaultRepairer returns a Repairer wired with the production implementations.
func DefaultRepairer() Repairer {
	return Repairer{
		InstallFn:         defaultInstallRepair,
		HooksFn:           defaultHooksRepair,
		ManifestFn:        defaultManifestRepair,
		FollowupsRenderFn: defaultFollowupsRenderRepair,
		HomeFn:            resolveHome,
		IsRepoDevFn:       defaultIsRepoDev,
		RepoRootFn:        defaultRepoRoot,
	}
}

// Repair is the production entry point: DefaultRepairer().Repair(...).
func Repair(results []Result, opts Opts, p Prompter, out io.Writer) RepairSummary {
	return DefaultRepairer().Repair(results, opts, p, out)
}

func defaultIsRepoDev() (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	return IsRepoDev(cwd)
}

func defaultRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return gitToplevel(cwd)
}

// Repair drives the interactive fix loop over every WARN and FAIL result,
// prompting once per auto-fixable item. All output goes to out, prompts
// included; the prompter reads from its own input source.
func (rp Repairer) Repair(results []Result, _ Opts, p Prompter, out io.Writer) RepairSummary {
	var summary RepairSummary
	runAll := false

	actionable := filterActionable(results)

	for i, r := range actionable {
		fmt.Fprintf(out, "\n[%d] %s %s: %s\n", r.Index, string(r.Severity), r.Name, r.Detail)

		plan, fixable := repairPlan(r)
		fmt.Fprintf(out, "repair plan: %s\n", plan)

		if !fixable {
			summary.NonFixable++
			continue
		}

		var decision Decision
		if runAll {
			decision = DecisionYes
		} else {
			decision = p.Confirm("apply? [y/N/a/q]: ")
		}

		switch decision {
		case DecisionQuit:
			summary.Skipped += len(actionable) - i
			return summarizeAndPrint(summary, out)
		case DecisionAbort:
			fmt.Fprintln(out, "Aborted.")
			summary.Skipped += len(actionable) - i
			return summarizeAndPrint(summary, out)
		case DecisionNo:
			summary.Skipped++
		case DecisionAll:
			runAll = true
			fallthrough
		case DecisionYes:
			fixSummary, err := rp.applyRepair(r, p, out)
			if err != nil {
				if err == errNonFixable {
					summary.NonFixable++
				} else {
					fmt.Fprintf(out, "  repair failed: %v\n", err)
					summary.Skipped++
				}
			} else {
				summary.Applied++
				fmt.Fprintf(out, "✓ fixed: %s\n", fixSummary)
			}
		}
	}

	return summarizeAndPrint(summary, out)
}

func filterActionable(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Severity == WARN || r.Severity == FAIL {
			out = append(out, r)
		}
	}
	return out
}

func summarizeAndPrint(s RepairSummary, out io.Writer) RepairSummary {
	repairWord := "repairs"
	if s.Applied == 1 {
		repairWord = "repair"
	}
	fmt.Fprintf(out, "\n%d %s applied, %d skipped, %d non-fixable.\n",
		s.Applied, repairWord, s.Skipped, s.NonFixable)
	return s
}

// repairPlan returns a human-readable plan description and whether auto-fix is available.
func repairPlan(r Result) (plan string, fixable bool) {
	switch r.Name {
	case "install":
		return "run `atomic claude install --merge` to re-sync bundle", true
	case "hooks":
		return "run `atomic hooks install` to register session-start hook", true
	case "signals":
		return "cannot auto-fix — run /refresh-wiki from Claude Code to refresh signals.", false
	case "refs":
		return "append @-ref block to a candidate file (axiom 4 selection)", true
	case "manifest":
		return "run `make -C atomic bundle` to regenerate embedded bundle", true
	case "followups":
		// Only the INDEX-sync subcase is auto-fixable; stale entries and invalid
		// frontmatter need the user.
		if strings.Contains(r.Detail, "atomic followups render") || strings.Contains(r.Detail, "INDEX.md") {
			return "run `atomic followups render` to regenerate INDEX.md", true
		}
		return "cannot auto-fix — " + r.Detail, false
	case "memory":
		return "cannot auto-fix — user-authored; orphan refs: " + r.Detail, false
	case "binary":
		return "cannot auto-fix — run `atomic update` to update.", false
	case "config":
		return "cannot auto-fix — edit config.toml or run `atomic config unset <key>`", false
	case "profile":
		return "run `atomic claude install` to create the profile stub; @-ref insertion is bundle-source-driven and updates with `atomic claude install/update`", false
	default:
		return "cannot auto-fix — unknown category", false
	}
}

// errNonFixable marks a nominally-fixable check that cannot be repaired in
// this context. Callers count it as NonFixable, not Skipped.
var errNonFixable = fmt.Errorf("cannot auto-fix in this context")

// applyRepair runs the repair for a fixable category and returns a summary line.
func (rp Repairer) applyRepair(r Result, p Prompter, out io.Writer) (string, error) {
	switch r.Name {
	case "install":
		if err := rp.InstallFn(out); err != nil {
			return "", err
		}
		return "re-synced bundle via `atomic claude install --merge`", nil
	case "hooks":
		if err := rp.HooksFn(out); err != nil {
			return "", err
		}
		return "session-start hook registered", nil
	case "refs":
		chosenFile, err := rp.applyRefsRepair(p, out)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("appended @-ref to %s", chosenFile), nil
	case "manifest":
		if err := rp.applyManifestRepairWithGuard(out); err != nil {
			return "", err
		}
		return "bundle regenerated via `make -C atomic bundle`", nil
	case "followups":
		if err := rp.applyFollowupsRepair(r, out); err != nil {
			return "", err
		}
		return "INDEX.md regenerated via `atomic followups render`", nil
	case "profile":
		// Unreachable while repairPlan reports profile as non-fixable; erroring
		// keeps a future fixable=true from silently no-opping.
		return "", fmt.Errorf("profile repair not yet implemented — run 'atomic claude install' instead")
	default:
		return "", fmt.Errorf("no repair for %q", r.Name)
	}
}

// applyFollowupsRepair dispatches to render based on the detail string.
func (rp Repairer) applyFollowupsRepair(r Result, out io.Writer) error {
	if strings.Contains(r.Detail, "atomic followups render") || strings.Contains(r.Detail, "INDEX.md") {
		return rp.FollowupsRenderFn(out)
	}
	return fmt.Errorf("no auto-fix available for this followups condition")
}

// applyManifestRepairWithGuard returns errNonFixable outside the atomic-claude
// repo, where the bundle regen has nothing to act on.
func (rp Repairer) applyManifestRepairWithGuard(out io.Writer) error {
	inRepoDev, err := rp.IsRepoDevFn()
	if err != nil {
		return fmt.Errorf("check repo-dev: %w", err)
	}
	if !inRepoDev {
		fmt.Fprintln(out, "  manifest repair only available inside the atomic-claude repo")
		return errNonFixable
	}
	return rp.ManifestFn(out)
}

const refsBlock = "\n## Project wiki (auto-loaded)\n\n@docs/wiki/index.md\n"

// applyRefsRepair appends the @-ref block to a candidate file and returns its
// name. The user picks only when more than one candidate exists.
func (rp Repairer) applyRefsRepair(p Prompter, out io.Writer) (string, error) {
	root := rp.RepoRootFn()

	// Dedupe by inode: on case-insensitive filesystems CLAUDE.md and claude.md
	// both stat but are the same file.
	var existing []string
	seenInode := make(map[uint64]bool)
	for _, name := range candidateFiles {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		key := inodeKey(info)
		if seenInode[key] {
			continue
		}
		seenInode[key] = true
		existing = append(existing, name)
	}

	var chosenFile string

	switch len(existing) {
	case 0:
		// Nothing to pick from; the outer prompt already confirmed the write.
		chosenFile = "CLAUDE.md"
	case 1:
		chosenFile = existing[0]
	default:
		items := make([]string, len(existing))
		copy(items, existing)
		idx := p.Indexed(items)
		if idx < 1 || idx > len(items) {
			return "", fmt.Errorf("no file selected")
		}
		chosenFile = items[idx-1]
	}

	targetPath := filepath.Join(root, chosenFile)
	fmt.Fprintf(out, "$ appending @-refs to %s\n", chosenFile)
	if err := appendRefsIfMissing(targetPath); err != nil {
		return "", err
	}
	return chosenFile, nil
}

// appendRefsIfMissing appends the @-ref block unless already present.
// Idempotent; an absent file is treated as empty.
func appendRefsIfMissing(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(raw)

	if strings.Contains(content, signalsRef) {
		return nil
	}

	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += refsBlock

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
