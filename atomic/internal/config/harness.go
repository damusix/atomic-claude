package config

import (
	"os"
	"path/filepath"
	"sync"
)

// harnessDirOnce resolves harnessDirCached exactly once per process, unless
// harnessDirOverride is set (test seam — takes priority and never touches
// os.UserHomeDir or the Once at all).
var (
	harnessDirOnce     sync.Once
	harnessDirCached   string
	harnessDirOverride *string
)

// harnessDir resolves the effective harness.dir value: the user config's
// harness.dir if set, else the built-in default. Resolved once per process
// and cached — repo-local path helpers call this on every invocation, so
// re-reading config.toml each time would be wasteful.
func harnessDir() string {
	if harnessDirOverride != nil {
		return *harnessDirOverride
	}
	harnessDirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			harnessDirCached = harnessDirDefault
			return
		}
		harnessDirCached = resolveHarnessDirFromHome(home)
	})
	return harnessDirCached
}

// resolveHarnessDirFromHome loads home's user config and returns harness.dir
// if set, else the built-in default. Lenient on any load error — a missing
// or unparseable config.toml falls back to the default rather than failing
// repo-local path resolution. Also re-validates a non-empty stored value with
// the same rules Set enforces at write time: Load itself does not validate,
// so a config.toml hand-edited (or corrupted) into an invalid shape like ".."
// would otherwise reach filepath.Join unchecked in the repo-local helpers —
// a path-escape risk. Takes home as a parameter (never calls os.UserHomeDir
// itself) so it can be exercised directly against a temp home.
func resolveHarnessDirFromHome(home string) string {
	cfg, _, err := Load(TOMLPath(home))
	if err != nil {
		return harnessDirDefault
	}
	if cfg.Harness.Dir == "" {
		return harnessDirDefault
	}
	if err := validateHarnessDir(cfg.Harness.Dir); err != nil {
		return harnessDirDefault
	}
	return cfg.Harness.Dir
}

// SetHarnessDirForTest overrides the effective harness.dir for tests,
// bypassing harnessDirOnce and os.UserHomeDir entirely so tests never touch
// the real home. Returns a restore func that puts back the previous override
// (nil if none was active) — callers should defer it immediately.
func SetHarnessDirForTest(dir string) func() {
	prev := harnessDirOverride
	harnessDirOverride = &dir
	return func() { harnessDirOverride = prev }
}

// ScratchpadDir returns <root>/<harness.dir>/.scratchpad.
func ScratchpadDir(root string) string {
	return filepath.Join(root, harnessDir(), ".scratchpad")
}

// ProjectDir returns <root>/<harness.dir>/project.
func ProjectDir(root string) string {
	return filepath.Join(root, harnessDir(), "project")
}

// FollowupsDir returns <root>/<harness.dir>/project/followups.
func FollowupsDir(root string) string {
	return filepath.Join(ProjectDir(root), "followups")
}

// IndexDir returns <root>/<harness.dir>/.atomic-index.
func IndexDir(root string) string {
	return filepath.Join(root, harnessDir(), ".atomic-index")
}

// IndexDBPath returns <root>/<harness.dir>/.atomic-index/atomic.db.
func IndexDBPath(root string) string {
	return filepath.Join(IndexDir(root), "atomic.db")
}

// WorktreesDir returns <root>/<harness.dir>/worktrees.
func WorktreesDir(root string) string {
	return filepath.Join(root, harnessDir(), "worktrees")
}

// RepoConfigPath returns <root>/<harness.dir>/atomic.toml — the repo-scoped
// config file loaded by LoadRepoConfig.
func RepoConfigPath(root string) string {
	return filepath.Join(root, harnessDir(), "atomic.toml")
}

// RemindersDir returns <root>/<harness.dir>/.scratchpad/reminders.
func RemindersDir(root string) string {
	return filepath.Join(ScratchpadDir(root), "reminders")
}
