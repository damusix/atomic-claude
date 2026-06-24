package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// resolveClaudeHome returns the resolved ~/.claude path.
func resolveClaudeHome() (string, error) {
	return claudeinstall.ResolveTarget("~/.claude")
}

// applyInstallRepair runs claudeinstall.Install (which is idempotent) against ~/.claude.
// This mirrors `atomic claude install --merge` behavior: unchanged files are no-ops,
// changed files get backed up and overwritten, CLAUDE.md gets block-aware handling
// (in-place <atomic> block replacement, or the proposed-file path when no block parses).
func applyInstallRepair(targetDir string) error {
	plan, err := claudeinstall.Install(targetDir, false, claudeinstall.RealClock)
	if err != nil {
		return fmt.Errorf("install plan: %w", err)
	}
	return claudeinstall.Apply(targetDir, plan, false, claudeinstall.RealClock)
}

// applyHooksRepair calls hooks.Install using the user-scope root ($HOME).
func applyHooksRepair() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home: %w", err)
	}
	// Repo root for the hook script: we use the cwd's git toplevel.
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	repoRoot := gitToplevel(cwd)
	return hooks.Install(repoRoot, home)
}

// defaultInstallRepair is the production install repair: prints the command then applies.
func defaultInstallRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic claude install --merge")
	target, err := resolveClaudeHome()
	if err != nil {
		return err
	}
	return applyInstallRepair(target)
}

// defaultHooksRepair is the production hooks repair: prints the command then applies.
func defaultHooksRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic hooks install")
	return applyHooksRepair()
}

// defaultFollowupsRenderRepair shells out to `atomic followups render`.
func defaultFollowupsRenderRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic followups render")
	return applyFollowupsRenderRepair(out)
}

// applyFollowupsRenderRepair runs `atomic followups render` from the git toplevel,
// streaming combined output to out.
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

// defaultManifestRepair is the production manifest repair: prints the command then applies,
// streaming make's combined output to out.
func defaultManifestRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ make -C atomic bundle")
	return applyManifestRepair(out)
}

// applyManifestRepair runs `make -C atomic bundle` from the git toplevel,
// streaming combined stdout+stderr to out.
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

// defaultConfigRepair re-renders config.resolved.md from the current TOML.
// Called by Repairer.ConfigFn in production.
func defaultConfigRepair(claudeHome string) error {
	return RunConfigRepairWith(claudeHome)
}
