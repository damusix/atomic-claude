package claudeinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// PruneDiff returns the subset of storedTargets absent from currentTargets, or
// nil for an empty storedTargets (a pre-framework install).
func PruneDiff(storedTargets []string, currentTargets map[string]bool) []string {
	if len(storedTargets) == 0 {
		return nil
	}
	var stale []string
	for _, t := range storedTargets {
		if !currentTargets[t] {
			stale = append(stale, t)
		}
	}
	return stale
}

// defaultPruneConfirm asks for interactive, batched approval.
func defaultPruneConfirm(stale []string) (bool, error) {
	desc := fmt.Sprintf(
		"The following %d artifact(s) were previously installed by atomic but are no longer in the current bundle:\n",
		len(stale),
	)
	for _, s := range stale {
		desc += "  • " + s + "\n"
	}
	desc += "Remove them?"
	ok, err := prompt.Confirm("Prune stale artifacts", desc, false)
	if errors.Is(err, prompt.ErrNonInteractive) {
		return false, nil
	}
	if errors.Is(err, prompt.ErrAborted) {
		// Ctrl+C is a decline, not an error.
		return false, nil
	}
	return ok, err
}

// DefaultPruneConfirm lets tests restore PruneConfirm after overriding it.
var DefaultPruneConfirm = defaultPruneConfirm

// PruneConfirm is a test seam: stubbed so tests never spawn a TTY.
var PruneConfirm = defaultPruneConfirm

// storedTargetSlice flattens cfg.Install.Artifacts across all kinds, or returns
// nil when nothing is stored.
func storedTargetSlice(cfg *config.Config) []string {
	var all []string
	all = append(all, cfg.Install.Artifacts.Agents...)
	all = append(all, cfg.Install.Artifacts.Commands...)
	all = append(all, cfg.Install.Artifacts.Skills...)
	all = append(all, cfg.Install.Artifacts.OutputStyles...)
	all = append(all, cfg.Install.Artifacts.Rules...)
	if len(all) == 0 {
		return nil
	}
	return all
}

// installedTargetSetFromConfig indexes cfg.Install.Artifacts, or returns nil
// when nothing is stored so callers apply no scoping.
func installedTargetSetFromConfig(cfg *config.Config) map[string]bool {
	stored := storedTargetSlice(cfg)
	if len(stored) == 0 {
		return nil
	}
	m := make(map[string]bool, len(stored))
	for _, t := range stored {
		m[t] = true
	}
	return m
}

func currentBundleTargetSet() map[string]bool {
	artifacts := embedded.Manifest()
	m := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		m[a.Target] = true
	}
	return m
}

// writeInstallManifest persists the [install] section to config.toml. Non-dry-run
// installs only. "claude-md" artifacts are not tracked in install.artifacts.
func writeInstallManifest(home string, plan []FileAction, retained []string) error {
	cfgPath := config.TOMLPath(home)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config for manifest write: %w", err)
	}

	// "dev" (the un-ldflagged default) is not parseable semver, and
	// config.Validate requires one — recording it would permanently fail every
	// dev contributor's `atomic doctor`.
	if version.Version != "dev" {
		cfg.Install.Version = version.Version
	}

	cfg.Install.Adopted = false
	keep := make(map[string]bool, len(retained))
	for _, target := range retained {
		keep[target] = true
	}
	retain := func(targets []string) []string {
		if len(keep) == 0 {
			return nil
		}
		return slices.DeleteFunc(targets, func(target string) bool { return !keep[target] })
	}
	cfg.Install.Artifacts.Agents = retain(cfg.Install.Artifacts.Agents)
	cfg.Install.Artifacts.Commands = retain(cfg.Install.Artifacts.Commands)
	cfg.Install.Artifacts.Skills = retain(cfg.Install.Artifacts.Skills)
	cfg.Install.Artifacts.OutputStyles = retain(cfg.Install.Artifacts.OutputStyles)
	cfg.Install.Artifacts.Rules = retain(cfg.Install.Artifacts.Rules)

	for _, fa := range plan {
		t := fa.Artifact.Target
		switch fa.Artifact.Kind {
		case "agent":
			cfg.Install.Artifacts.Agents = append(cfg.Install.Artifacts.Agents, t)
		case "command":
			cfg.Install.Artifacts.Commands = append(cfg.Install.Artifacts.Commands, t)
		case "skill":
			cfg.Install.Artifacts.Skills = append(cfg.Install.Artifacts.Skills, t)
		case "output-style":
			cfg.Install.Artifacts.OutputStyles = append(cfg.Install.Artifacts.OutputStyles, t)
		case "rule":
			cfg.Install.Artifacts.Rules = append(cfg.Install.Artifacts.Rules, t)
			// "claude-md" intentionally omitted — not tracked in install.artifacts.
		}
	}

	return config.WritePersist(cfgPath, cfg)
}

// runPrune removes stale paths only with approval and returns those still installed.
func runPrune(targetDir string, stale []string) ([]string, error) {
	if len(stale) == 0 {
		return nil, nil
	}
	ok, err := PruneConfirm(stale)
	if err != nil && !errors.Is(err, prompt.ErrAborted) {
		return nil, fmt.Errorf("prune confirm: %w", err)
	}
	if err != nil || !ok {
		var retained []string
		for _, t := range stale {
			_, err := os.Lstat(filepath.Join(targetDir, filepath.FromSlash(t)))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("check stale artifact %s: %w", t, err)
			}
			retained = append(retained, t)
		}
		return retained, nil
	}
	for _, t := range stale {
		p := filepath.Join(targetDir, filepath.FromSlash(t))
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("prune %s: %w", t, err)
		}
	}
	return nil, nil
}
