// Package config manages atomic's TOML-backed configuration under ~/.atomic/:
// lenient load, strict write validation, get/set/unset, atomic file writes, and
// a markdown render of resolved values.
package config

import "path/filepath"

// Dir returns <home>/.atomic — the root of atomic-owned state.
func Dir(home string) string {
	return filepath.Join(home, ".atomic")
}

// TOMLPath returns the path to the user config file.
func TOMLPath(home string) string {
	return filepath.Join(Dir(home), "config.toml")
}

// BackupDir returns the directory where claudeinstall writes pre-write backups.
func BackupDir(home string) string {
	return filepath.Join(Dir(home), "backups")
}

// ProposedCLAUDEMD returns the path where claudeinstall writes a diverged
// CLAUDE.md for the user to review and merge.
func ProposedCLAUDEMD(home string) string {
	return filepath.Join(Dir(home), "proposed", "CLAUDE.md")
}

// PreInstallDir returns the directory where claudeinstall writes a write-once
// snapshot of every file it will touch, captured before the first Apply() call.
func PreInstallDir(home string) string {
	return filepath.Join(Dir(home), "pre-install")
}

// ProfilePath returns the user profile file. It is @-referenced from CLAUDE.md so
// every session loads it: created idempotently at install time, written
// opportunistically by Claude.
func ProfilePath(home string) string {
	return filepath.Join(Dir(home), "profile.md")
}

// ProfileRelPath returns profile.md's home-relative path with forward slashes,
// matching how pre-install manifests store it. Compare against manifest entries
// through this, never a hardcoded string.
func ProfileRelPath() string {
	return ".atomic/profile.md"
}

// StatePath returns ~/.atomic/state.json — the machine-managed source of truth
// for update-check cadence, staged downloads, and swap-lock coordination.
func StatePath(home string) string {
	return filepath.Join(Dir(home), "state.json")
}
