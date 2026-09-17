package config

import (
	"path/filepath"
)

// harnessDirOverride is the test seam over the repository-state segment. It
// takes priority over every neutral rung and touches neither os.UserHomeDir nor
// the process caches. SetHarnessDirForTest installs it.
var harnessDirOverride *string

// resolveHarnessDirFromHome returns home's configured repository-state segment:
// state.dir when set, else a non-default legacy harness.dir as migration
// evidence, else the built-in default. It never reads environment fingerprints —
// repository-state resolution is harness-neutral. home is a parameter so this
// can be exercised against a temp home.
func resolveHarnessDirFromHome(home string) string {
	return segmentFromConfig(TOMLPath(home))
}

// segmentFromConfig reads one config file and applies the same evidence rules as
// readConfiguredStateDirSegment.
func segmentFromConfig(path string) string {
	cfg, _, err := Load(path)
	if err != nil {
		return harnessDirDefault
	}
	if cfg.State.Dir != "" && ValidateStateDirSegment(cfg.State.Dir) == nil {
		return cfg.State.Dir
	}
	if cfg.Harness.Dir != "" && cfg.Harness.Dir != harnessDirDefault && validateHarnessDir(cfg.Harness.Dir) == nil {
		return cfg.Harness.Dir
	}
	return harnessDirDefault
}

// SetHarnessDirForTest overrides the effective repository-state segment,
// bypassing the neutral resolver so tests never touch the real home. Returns a
// restore func (nil if no override was active) that the caller should defer
// immediately.
func SetHarnessDirForTest(dir string) func() {
	prev := harnessDirOverride
	harnessDirOverride = &dir
	return func() { harnessDirOverride = prev }
}

// stateRoot returns <root>/<repository-state dir>, resolved through the neutral
// ladder. root may be empty, in which case the resolved segment is returned
// relative — the form callers use to write repo-relative paths.
func stateRoot(root string) string {
	return repositoryStateDir(root)
}

// ScratchpadDir returns <state-root>/.scratchpad.
func ScratchpadDir(root string) string {
	return filepath.Join(stateRoot(root), ".scratchpad")
}

// ProjectDir returns <state-root>/project.
func ProjectDir(root string) string {
	return filepath.Join(stateRoot(root), "project")
}

// FollowupsDir returns <state-root>/project/followups.
func FollowupsDir(root string) string {
	return filepath.Join(ProjectDir(root), "followups")
}

// IndexDir returns <state-root>/.atomic-index.
func IndexDir(root string) string {
	return filepath.Join(stateRoot(root), ".atomic-index")
}

// IndexDBPath returns <state-root>/.atomic-index/atomic.db.
func IndexDBPath(root string) string {
	return filepath.Join(IndexDir(root), "atomic.db")
}

// WorktreesDir returns <state-root>/worktrees.
func WorktreesDir(root string) string {
	return filepath.Join(stateRoot(root), "worktrees")
}

// RepoConfigPath returns <state-root>/atomic.toml — the repo-scoped config file
// LoadRepoConfig reads.
func RepoConfigPath(root string) string {
	return filepath.Join(stateRoot(root), "atomic.toml")
}

// RemindersDir returns the project-keyed reminders directory. RemindersDirLegacy
// resolves the pre-relocation path this used to return directly.
func RemindersDir(root string) string {
	return ProjectRemindersDir(root)
}
