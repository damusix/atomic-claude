// Package config manages atomic's TOML-backed configuration stored under
// ~/.claude/.atomic/. It provides lenient load, strict write validation,
// get/set/unset, atomic file writes, and a markdown render of resolved values.
package config

import "path/filepath"

// Dir returns <claudeHome>/.atomic — the root of atomic-owned state.
func Dir(claudeHome string) string {
	return filepath.Join(claudeHome, ".atomic")
}

// TOMLPath returns the path to the user config file.
func TOMLPath(claudeHome string) string {
	return filepath.Join(Dir(claudeHome), "config.toml")
}

// ResolvedPath returns the path to the rendered markdown snapshot.
// This file is @-referenced from CLAUDE.md so every Claude session sees it.
func ResolvedPath(claudeHome string) string {
	return filepath.Join(Dir(claudeHome), "config.resolved.md")
}

// BackupDir returns the directory where claudeinstall writes pre-write backups.
func BackupDir(claudeHome string) string {
	return filepath.Join(Dir(claudeHome), "backups")
}

// ProposedCLAUDEMD returns the path where claudeinstall writes a diverged
// CLAUDE.md for the user to review and merge.
func ProposedCLAUDEMD(claudeHome string) string {
	return filepath.Join(Dir(claudeHome), "proposed", "CLAUDE.md")
}

// PreInstallDir returns the directory where claudeinstall writes a write-once
// snapshot of every file it will touch, captured before the first Apply() call.
func PreInstallDir(claudeHome string) string {
	return filepath.Join(Dir(claudeHome), "pre-install")
}
