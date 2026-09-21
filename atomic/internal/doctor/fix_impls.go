package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/install"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

func resolveClaudeHome() (string, error) {
	return claudeinstall.ResolveTarget("~/.claude")
}

// resolveHome returns the root of atomic-owned config state (~/.atomic),
// distinct from resolveClaudeHome's ~/.claude install target.
func resolveHome() (string, error) {
	return os.UserHomeDir()
}

// applyInstallRepair mirrors `atomic claude install --merge`: idempotent, so
// unchanged files no-op and changed ones are backed up before being overwritten.
func applyInstallRepair(targetDir, home string) error {
	plan, err := claudeinstall.Install(targetDir, home, false, claudeinstall.RealClock)
	if err != nil {
		return fmt.Errorf("install plan: %w", err)
	}
	return claudeinstall.Apply(targetDir, home, plan, false, claudeinstall.RealClock)
}

// applyHooksRepair calls hooks.Install using the user-scope root ($HOME).
func applyHooksRepair() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	repoRoot := gitToplevel(cwd)
	skipped, err := hooks.Install(repoRoot, home)
	if err != nil {
		return err
	}
	if skipped {
		return fmt.Errorf("settings.json is read-only; hooks not installed")
	}
	return nil
}

func defaultInstallRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic claude install --merge")
	target, err := resolveClaudeHome()
	if err != nil {
		return err
	}
	home, err := resolveHome()
	if err != nil {
		return err
	}
	return applyInstallRepair(target, home)
}

func defaultHooksRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic hooks install")
	return applyHooksRepair()
}

func defaultFollowupsRenderRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic followups render")
	return applyFollowupsRenderRepair(out)
}

// applyFollowupsRenderRepair shells out from the git toplevel, streaming
// combined output to out.
func applyFollowupsRenderRepair(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	root := gitToplevel(cwd)
	cmd := exec.Command("atomic", "followups", "render")
	cmd.Dir = root
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("atomic followups render: %w", err)
	}
	return nil
}

// defaultOutputStyleRepair seeds the user-level outputStyle key, mirroring
// `atomic claude install`'s trigger. It never writes a project settings file.
func defaultOutputStyleRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ seed user-level outputStyle")
	target, err := resolveClaudeHome()
	if err != nil {
		return err
	}
	home, err := resolveHome()
	if err != nil {
		return err
	}
	wrote, err := hooks.SeedOutputStyleInDir(target, target, home)
	if err != nil {
		return err
	}
	if !wrote {
		// repairPlan gates output-style as fixable only when a write is
		// actually possible; reaching here means the state changed out from
		// under the check (key added, flag flipped) between plan and apply.
		fmt.Fprintln(out, "  no-op: output_style.seed is disabled, key already set, or style file not installed")
		return errNonFixable
	}
	return nil
}

func defaultManifestRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ make -C atomic bundle")
	return applyManifestRepair(out)
}

// applyManifestRepair shells out from the git toplevel, streaming combined
// stdout+stderr to out.
func applyManifestRepair(out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	root := gitToplevel(cwd)
	atomicDir := filepath.Join(root, "atomic")
	cmd := exec.Command("make", "-C", atomicDir, "bundle")
	cmd.Dir = root
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make -C atomic bundle: %w", err)
	}
	return nil
}

// convergeStepsFn builds the install-engine steps a ledger-managed repair runs.
// Tests swap it to inject a registry whose adapter records lock acquisition,
// proving the doctor repair serializes against update and uninstall.
var convergeStepsFn = install.DefaultSteps

// defaultConvergeRepair reuses the CP7A install engine's converge planner for a
// ledger-managed doctor repair. The planner resolves every enrolled target,
// re-observes each projection, and hands mutation to the adapter, which acquires
// the one advisory lifecycle lock and recovers unresolved journals oldest-first.
// The doctor loop already obtained per-item consent, so AssumeYes carries that
// recorded consent into the planner rather than prompting a second time.
func defaultConvergeRepair(home string, out io.Writer) error {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return fmt.Errorf("read ledger: %w", err)
	}
	if len(ledger.Targets) == 0 {
		fmt.Fprintln(out, "  no enrolled targets to converge")
		return errNonFixable
	}

	steps := convergeStepsFn(home)
	steps.Home = home
	steps.AssumeYes = true
	reports, err := steps.Converge(install.ConvergeRequest{Selection: install.Selection{EnrolledOnly: true}})
	if err != nil {
		return err
	}
	blocked := false
	for _, report := range reports {
		switch {
		case len(report.Blockers) > 0:
			blocked = true
			fmt.Fprintf(out, "  %s: %s (%s)\n", report.Target.Key(), report.Status, strings.Join(report.Blockers, "; "))
		case report.Applied:
			fmt.Fprintf(out, "  %s: %s applied\n", report.Target.Key(), report.Status)
		default:
			fmt.Fprintf(out, "  %s: %s\n", report.Target.Key(), report.Status)
		}
	}
	if blocked {
		// A plan the converge engine refused to mutate is not a repair. Mirror
		// the install verb, which treats any blocked target as failed convergence,
		// so the fix loop counts it non-fixable rather than reporting "fixed".
		return errNonFixable
	}
	return nil
}
