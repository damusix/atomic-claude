package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// harnessDirOnce resolves harnessDirCached once per process, unless
// harnessDirOverride is set — the test seam takes priority and touches neither
// os.UserHomeDir nor the Once.
var (
	harnessDirOnce     sync.Once
	harnessDirCached   string
	harnessDirOverride *string
)

// harnessDir resolves the effective harness.dir via resolveHarnessDir's ladder,
// cached per process: repo-local path helpers call this on every invocation, and
// env is process-stable, so re-reading config.toml each time would be waste.
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
		harnessDirCached = resolveHarnessDir(home)
	})
	return harnessDirCached
}

// resolveHarnessDir walks a five-rung ladder, most specific first:
//
//  1. ATOMIC_HARNESS env — a leading "." is tolerated and normalized; an
//     invalid value falls through rather than erroring.
//  2. PI_CODING_AGENT == "true" → ".pi"
//  3. CLAUDECODE == "1" → ".claude"
//  4. the user config's harness.dir, if set
//  5. the built-in default
//
// Rung 2 precedes rung 3 deliberately: a pi agent launched from within Claude
// Code exposes both fingerprints, and PI_CODING_AGENT is the more specific one.
func resolveHarnessDir(home string) string {
	if raw := os.Getenv("ATOMIC_HARNESS"); raw != "" {
		dir := "." + strings.TrimPrefix(raw, ".")
		if validateHarnessDir(dir) == nil {
			return dir
		}
	}
	if os.Getenv("PI_CODING_AGENT") == "true" {
		return ".pi"
	}
	if os.Getenv("CLAUDECODE") == "1" {
		return ".claude"
	}
	return resolveHarnessDirFromHome(home)
}

// resolveHarnessDirFromHome returns home's configured harness.dir, else the
// default. Lenient on any load error. It re-validates a stored value with the
// rules Set enforces at write time, because Load itself does not validate — a
// config.toml hand-edited into something like ".." would otherwise reach
// filepath.Join unchecked in the repo-local helpers. home is a parameter so this
// can be exercised against a temp home.
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

// SetHarnessDirForTest overrides the effective harness.dir, bypassing the Once
// and os.UserHomeDir so tests never touch the real home. Returns a restore func
// (nil if no override was active) that the caller should defer immediately.
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
// config file LoadRepoConfig reads.
func RepoConfigPath(root string) string {
	return filepath.Join(root, harnessDir(), "atomic.toml")
}

// RemindersDir returns <root>/<harness.dir>/.scratchpad/reminders.
func RemindersDir(root string) string {
	return filepath.Join(ScratchpadDir(root), "reminders")
}
