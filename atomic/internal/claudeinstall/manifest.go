package claudeinstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// PruneDiff returns the subset of storedTargets that are absent from currentTargets.
// Exported for direct unit-testing. Pure function: no filesystem I/O, no side effects.
// Returns nil when storedTargets is empty — pre-framework install with no [install.artifacts].
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

// defaultPruneConfirm is the production confirm: huh-backed interactive batched confirm.
// Non-interactive terminals (no TTY) receive ErrNonInteractive → prune is silently skipped.
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
		// Non-interactive terminal — skip prune silently; no human to confirm.
		return false, nil
	}
	if errors.Is(err, prompt.ErrAborted) {
		// User pressed Ctrl+C at the prompt — treat as a decline, not an error.
		return false, nil
	}
	return ok, err
}

// DefaultPruneConfirm is the production PruneConfirm implementation.
// Exported so tests can restore the original after overriding PruneConfirm.
var DefaultPruneConfirm = defaultPruneConfirm

// PruneConfirm is the injectable seam for the interactive batched confirm during prune.
// Production code uses defaultPruneConfirm; tests override to avoid spawning a TTY.
var PruneConfirm = defaultPruneConfirm

// storedTargetSlice returns a flat slice of all artifact targets recorded in
// cfg.Install.Artifacts across all kinds.
// Returns nil when no artifacts are stored — pre-framework install or first-ever install.
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

// installedTargetSetFromConfig builds a map[string]bool of all targets in
// cfg.Install.Artifacts. Returns nil when no artifacts are stored — no scoping applied
// (pre-framework install: uninstall uses existing snapshot-only behavior).
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

// currentBundleTargetSet returns a set of all Target paths in the current embedded bundle.
func currentBundleTargetSet() map[string]bool {
	artifacts := embedded.Manifest()
	m := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		m[a.Target] = true
	}
	return m
}

// writeInstallManifest persists the [install] section to config.toml after a successful install.
// Reads the existing config leniently, updates Install.Version and Install.Artifacts, then
// atomically rewrites the file. "claude-md" kind artifacts are not tracked in install.artifacts.
// Must only be called on non-dry-run installs.
func writeInstallManifest(targetDir string, plan []FileAction) error {
	cfgPath := config.TOMLPath(targetDir)
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config for manifest write: %w", err)
	}

	cfg.Install.Version = version.Version

	// Reset per-kind lists before repopulating from the current plan.
	cfg.Install.Artifacts.Agents = nil
	cfg.Install.Artifacts.Commands = nil
	cfg.Install.Artifacts.Skills = nil
	cfg.Install.Artifacts.OutputStyles = nil
	cfg.Install.Artifacts.Rules = nil

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

// runPrune presents a batched confirm for the stale paths and, if approved, removes them.
// Returns the list of paths successfully removed.
// Safe to call with an empty or nil stale slice (no-op).
func runPrune(targetDir string, stale []string) ([]string, error) {
	if len(stale) == 0 {
		return nil, nil
	}
	ok, err := PruneConfirm(stale)
	if err != nil {
		if errors.Is(err, prompt.ErrAborted) {
			// User pressed Ctrl+C — treat as a decline, not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("prune confirm: %w", err)
	}
	if !ok {
		return nil, nil
	}
	var removed []string
	for _, t := range stale {
		p := filepath.Join(targetDir, filepath.FromSlash(t))
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return removed, fmt.Errorf("prune %s: %w", t, err)
		}
		removed = append(removed, t)
	}
	return removed, nil
}
