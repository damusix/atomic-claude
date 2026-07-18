// Package config manages atomic's TOML-backed configuration stored under
// ~/.atomic/. It provides lenient load, strict write validation,
// get/set/unset, atomic file writes, and a markdown render of resolved values.
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

// ResolvedPath returns the path to the rendered markdown snapshot.
// This file is @-referenced from CLAUDE.md so every Claude session sees it.
func ResolvedPath(home string) string {
	return filepath.Join(Dir(home), "config.resolved.md")
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

// ProfilePath returns the path to the user profile file.
// This file is @-referenced from CLAUDE.md so every Claude session can load it.
// It is created at install time (idempotent) and written to opportunistically by Claude.
func ProfilePath(home string) string {
	return filepath.Join(Dir(home), "profile.md")
}

// ProfileRelPath returns the home-relative path of profile.md using
// forward slashes (matching the format stored in pre-install manifests).
// Use this instead of a hardcoded string when comparing against manifest entries.
func ProfileRelPath() string {
	return ".atomic/profile.md"
}
