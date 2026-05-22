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
// changed files get backed up and overwritten, CLAUDE.md gets the proposed-file treatment.
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

// defaultFollowupsRenderRepair shells out to `atomic followups render`.
func defaultFollowupsRenderRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic followups render")
	return applyFollowupsRenderRepair()
}

// defaultFollowupsMigrateRepair shells out to `atomic followups migrate`.
func defaultFollowupsMigrateRepair(out io.Writer) error {
	fmt.Fprintln(out, "$ atomic followups migrate")
	return applyFollowupsMigrateRepair()
}

// applyFollowupsRenderRepair runs `atomic followups render` from the git toplevel.
func applyFollowupsRenderRepair() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	root := gitToplevel(cwd)
	cmd := exec.Command("atomic", "followups", "render")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("atomic followups render: %w\n%s", err, out)
	}
	return nil
}

// applyFollowupsMigrateRepair runs `atomic followups migrate` from the git toplevel.
func applyFollowupsMigrateRepair() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	root := gitToplevel(cwd)
	cmd := exec.Command("atomic", "followups", "migrate")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("atomic followups migrate: %w\n%s", err, out)
	}
	return nil
}

// applyManifestRepair runs `make -C atomic bundle` from the git toplevel.
func applyManifestRepair() error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	root := gitToplevel(cwd)
	atomicDir := filepath.Join(root, "atomic")
	cmd := exec.Command("make", "-C", atomicDir, "bundle")
	cmd.Dir = root
	cmd.Stdout = nil
	cmd.Stderr = nil
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("make -C atomic bundle: %w\n%s", err, out)
	}
	return nil
}
