package config

import (
	"os"
	"path/filepath"
	"strings"
)

// WriteAmbiguousStateForTest pins state.dir to .state in home's config and
// populates repo's .claude and .atomic, so repo's state root refuses.
func WriteAmbiguousStateForTest(home, repo string) error {
	cfg := Default()
	if err := Set(cfg, "state.dir", ".state"); err != nil {
		return err
	}
	if err := WritePersist(TOMLPath(home), cfg); err != nil {
		return err
	}
	for _, seg := range []string{".claude", ".atomic"} {
		if err := os.MkdirAll(filepath.Join(repo, seg, "project"), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// stateDirOverride is the test seam over the repository-state segment; it
// outranks every neutral rung and touches no home or process cache.
var stateDirOverride *string

// resolveHarnessDirFromHome returns state.dir when set, else the built-in
// default, never an environment fingerprint; tests pass their own home.
func resolveHarnessDirFromHome(home string) string {
	return segmentFromConfig(TOMLPath(home))
}

// segmentFromConfig reads state.dir from one config file; any load or
// validation failure yields the built-in default.
func segmentFromConfig(path string) string {
	cfg, _, err := Load(path)
	if err != nil {
		return stateDirDefault
	}
	if cfg.State.Dir != "" && ValidateStateDirPath(cfg.State.Dir) == nil {
		return cfg.State.Dir
	}
	return stateDirDefault
}

// SetStateDirForTest overrides the repository-state segment so tests never touch
// the real home. The caller should defer the returned restore func immediately.
func SetStateDirForTest(dir string) func() {
	prev := stateDirOverride
	stateDirOverride = &dir
	return func() { stateDirOverride = prev }
}

// stateRoot returns <root>/<repository-state dir>. An empty root yields the
// relative segment callers use for repo-relative paths.
func stateRoot(root string) string {
	dir, err := repositoryStateDir(root)
	if err != nil {
		stateRefusalExit(err)
		panic(err)
	}
	return dir
}

// RepoRelative returns abs relative to root, and false when abs lies outside
// root — a state root a record or ATOMIC_STATE_DIR placed elsewhere.
func RepoRelative(root, abs string) (string, bool) {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
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

// IndexDBName is the code-index database file inside an index directory.
const IndexDBName = "atomic.db"

// IndexDBPath returns <state-root>/.atomic-index/atomic.db.
func IndexDBPath(root string) string {
	return filepath.Join(IndexDir(root), IndexDBName)
}

// WorktreeDir is the one worktree helper: a preference path wins and "off" is
// returned verbatim; else .claude/worktrees for Claude, <state-root>/worktrees.
func WorktreeDir(root, harness, preference string) string {
	switch {
	case preference == "off":
		return preference
	case filepath.IsAbs(preference):
		return preference
	case preference != "":
		return filepath.Join(root, preference)
	case harness == "claude":
		return filepath.Join(root, stateDirLegacy, "worktrees")
	}
	return filepath.Join(stateRoot(root), "worktrees")
}

// RepoConfigPath returns <state-root>/atomic.toml — the repo-scoped config file
// LoadRepoConfig reads.
func RepoConfigPath(root string) string {
	return filepath.Join(stateRoot(root), "atomic.toml")
}

// WorkflowPrefsPath returns <state-root>/workflow-preferences.md. An ambiguous
// state root is an error, not an exit, so the resolver can skip the level.
func WorkflowPrefsPath(root string) (string, error) {
	dir, err := repositoryStateDir(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workflow-preferences.md"), nil
}
