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
// Tests inject a fakePrompter; production uses stdinPrompter.
type Prompter interface {
	// Confirm prompts the user and returns their decision.
	Confirm(prompt string) Decision
	// Indexed presents a numbered list and returns the 1-based index the user
	// chose (0 = cancel/none).
	Indexed(items []string) int
}

// RepairSummary is the outcome of a Repair call.
type RepairSummary struct {
	Applied    int
	Skipped    int
	NonFixable int
}

// -- injectable functions for testability --

// installRepairFn applies the install repair. Default uses claudeinstall.
var installRepairFn func(io.Writer) error = defaultInstallRepair

// hooksRepairFn applies the hooks repair. Default uses hooks.Install.
var hooksRepairFn func(io.Writer) error = defaultHooksRepair

// manifestRepairFn runs `make -C atomic bundle`. Default shells out.
var manifestRepairFn func(io.Writer) error = defaultManifestRepair

// followupsRenderRepairFn runs `atomic followups render` to regenerate INDEX.md.
var followupsRenderRepairFn func(io.Writer) error = defaultFollowupsRenderRepair

// isRepoDevFn checks whether cwd is in the atomic-claude repo.
var isRepoDevFn func() (bool, error) = defaultIsRepoDev

// repoRootFn returns the repo root for refs repair.
var repoRootFn func() string = defaultRepoRoot

// SetInstallRepairFn replaces the install repair function (testing only).
// Pass nil to restore the default.
func SetInstallRepairFn(fn func(io.Writer) error) {
	if fn == nil {
		installRepairFn = defaultInstallRepair
	} else {
		installRepairFn = fn
	}
}

// SetHooksRepairFn replaces the hooks repair function (testing only).
func SetHooksRepairFn(fn func(io.Writer) error) {
	if fn == nil {
		hooksRepairFn = defaultHooksRepair
	} else {
		hooksRepairFn = fn
	}
}

// SetManifestRepairFn replaces the manifest repair function (testing only).
func SetManifestRepairFn(fn func(io.Writer) error) {
	if fn == nil {
		manifestRepairFn = defaultManifestRepair
	} else {
		manifestRepairFn = fn
	}
}

// SetFollowupsRenderRepairFn replaces the followups render repair function (testing only).
func SetFollowupsRenderRepairFn(fn func(io.Writer) error) {
	if fn == nil {
		followupsRenderRepairFn = defaultFollowupsRenderRepair
	} else {
		followupsRenderRepairFn = fn
	}
}

// SetIsRepoDevFn replaces the repo-dev check (testing only).
func SetIsRepoDevFn(fn func() (bool, error)) {
	if fn == nil {
		isRepoDevFn = defaultIsRepoDev
	} else {
		isRepoDevFn = fn
	}
}

// SetRepoRootFn replaces the repo-root resolver (testing only).
func SetRepoRootFn(fn func() string) {
	if fn == nil {
		repoRootFn = defaultRepoRoot
	} else {
		repoRootFn = fn
	}
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

// -- default repair implementations --

func defaultInstallRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic claude install --merge")
	target, err := resolveClaudeHome()
	if err != nil {
		return err
	}
	// Import-cycle prevention: claudeinstall is used directly.
	// Plan + Apply in non-dry-run mode matches "install --merge" semantics.
	return applyInstallRepair(target)
}

func defaultHooksRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic hooks install")
	return applyHooksRepair()
}

func defaultManifestRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ make -C atomic bundle")
	return applyManifestRepair()
}

// Repair drives the interactive fix loop.
//
// For each result where Severity is WARN or FAIL:
//  1. Print the item header and repair plan.
//  2. For non-auto-fixable: print the instruction and count as NonFixable.
//  3. For auto-fixable: prompt; apply on Yes/All; skip on No/Quit.
//
// Prints a summary line at the end.
// The passed io.Writer receives all output (prompts are also written there;
// the prompter reads from its own input source).
func Repair(results []Result, _ Opts, p Prompter, out io.Writer) RepairSummary {
	var summary RepairSummary
	runAll := false // set when user chose 'a' (all)

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
			// Count this item and all remaining as skipped.
			summary.Skipped += len(actionable) - i
			return summarizeAndPrint(summary, out)
		case DecisionAbort:
			// Ctrl+C / huh ErrUserAborted — stop entire loop.
			fmt.Fprintln(out, "Aborted.")
			summary.Skipped += len(actionable) - i
			return summarizeAndPrint(summary, out)
		case DecisionNo:
			summary.Skipped++
		case DecisionAll:
			runAll = true
			fallthrough
		case DecisionYes:
			if err := applyRepair(r, p, out); err != nil {
				if err == errNonFixable {
					summary.NonFixable++
				} else {
					fmt.Fprintf(out, "  repair failed: %v\n", err)
					summary.Skipped++
				}
			} else {
				summary.Applied++
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
		return "cannot auto-fix — run /refresh-signals from Claude Code to refresh signals.", false
	case "refs":
		return "append @-ref block to a candidate file (axiom 4 selection)", true
	case "manifest":
		return "run `make -C atomic bundle` to regenerate embedded bundle", true
	case "followups":
		// INDEX-sync subcase is auto-fixable.
		// Stale entries and invalid frontmatter are not (require user action).
		if strings.Contains(r.Detail, "atomic followups render") || strings.Contains(r.Detail, "INDEX.md") {
			return "run `atomic followups render` to regenerate INDEX.md", true
		}
		return "cannot auto-fix — " + r.Detail, false
	case "memory":
		return "cannot auto-fix — user-authored; orphan refs: " + r.Detail, false
	case "binary":
		return "cannot auto-fix — run `atomic update` to update.", false
	case "config":
		// Parse errors and invalid values cannot be auto-fixed; user must edit.
		// Only drift (WARN) is fixable.
		return "re-render config.resolved.md from current config.toml", r.Severity == WARN
	default:
		return "cannot auto-fix — unknown category", false
	}
}

// errNonFixable is a sentinel returned when a nominally-fixable check cannot
// be repaired in this context (e.g. manifest repair outside the atomic-claude
// repo). Callers should increment NonFixable, not Skipped.
var errNonFixable = fmt.Errorf("cannot auto-fix in this context")

// applyRepair dispatches the actual repair for fixable categories.
func applyRepair(r Result, p Prompter, out io.Writer) error {
	switch r.Name {
	case "install":
		return installRepairFn(out)
	case "hooks":
		return hooksRepairFn(out)
	case "refs":
		return applyRefsRepair(p, out)
	case "manifest":
		return applyManifestRepairWithGuard(out)
	case "config":
		return applyConfigRepair(out)
	case "followups":
		return applyFollowupsRepair(r, out)
	default:
		return fmt.Errorf("no repair for %q", r.Name)
	}
}

// applyFollowupsRepair dispatches to render based on the detail string.
func applyFollowupsRepair(r Result, out io.Writer) error {
	if strings.Contains(r.Detail, "atomic followups render") || strings.Contains(r.Detail, "INDEX.md") {
		return followupsRenderRepairFn(out)
	}
	return fmt.Errorf("no auto-fix available for this followups condition")
}

// applyConfigRepair re-renders config.resolved.md from the current TOML.
func applyConfigRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ re-rendering config.resolved.md")
	claudeHome, err := resolveClaudeHome()
	if err != nil {
		return fmt.Errorf("resolve claude home: %w", err)
	}
	return configRepairFn(claudeHome)
}

// applyManifestRepairWithGuard checks repo-dev before delegating.
// Returns errNonFixable when not in the atomic-claude repo so the caller
// can increment NonFixable rather than Skipped.
func applyManifestRepairWithGuard(out io.Writer) error {
	inRepoDev, err := isRepoDevFn()
	if err != nil {
		return fmt.Errorf("check repo-dev: %w", err)
	}
	if !inRepoDev {
		fmt.Fprintln(out, "  manifest repair only available inside the atomic-claude repo")
		return errNonFixable
	}
	return manifestRepairFn(out)
}

// -- refs repair --

const refsBlock = "\n## Project signals (auto-loaded)\n\n@.claude/project/deterministic-signals.md\n@.claude/project/signals.md\n"

// applyRefsRepair appends the @-ref block to the chosen candidate file.
// Selection rules per brief:
//   - 0 existing candidates → default CLAUDE.md (create).
//   - 1 existing candidate → single Yes/No via Confirm.
//   - >1 existing candidates → Indexed numbered list.
func applyRefsRepair(p Prompter, out io.Writer) error {
	root := repoRootFn()

	// Collect which candidate files exist. Deduplicate by inode to handle
	// case-insensitive filesystems (macOS) where CLAUDE.md and claude.md both
	// stat successfully but refer to the same on-disk file.
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
		// Default to CLAUDE.md (create it). User already confirmed at the outer prompt.
		chosenFile = "CLAUDE.md"
	case 1:
		// Single candidate — user already confirmed at the outer level.
		chosenFile = existing[0]
	default:
		// Multiple candidates — numbered list per axiom 4.
		items := make([]string, len(existing))
		copy(items, existing)
		idx := p.Indexed(items)
		if idx < 1 || idx > len(items) {
			return fmt.Errorf("no file selected")
		}
		chosenFile = items[idx-1]
	}

	targetPath := filepath.Join(root, chosenFile)
	fmt.Fprintf(out, "$ appending @-refs to %s\n", chosenFile)
	return appendRefsIfMissing(targetPath)
}

// appendRefsIfMissing reads the file (or treats as empty if absent) and appends
// only the missing @-ref line(s). Idempotent.
//
// Cases:
//   - Both present → no-op.
//   - Neither present → append the full refsBlock (header + both refs).
//   - Det present, Inf missing → append only the inferred ref line.
//   - Inf present, Det missing → append only the deterministic ref line.
func appendRefsIfMissing(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	content := string(raw)

	hasDet := strings.Contains(content, deterministicSignalsRef)
	hasInf := strings.Contains(content, signalsRef)
	if hasDet && hasInf {
		// Already wired — nothing to do.
		return nil
	}

	// Ensure file ends with newline before appending.
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if !hasDet && !hasInf {
		// Neither present: append the full block with the section header.
		content += refsBlock
	} else if hasDet {
		// Det already there; only the inferred ref is missing.
		content += signalsRef + "\n"
	} else {
		// Inf already there; only the deterministic ref is missing.
		content += deterministicSignalsRef + "\n"
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
