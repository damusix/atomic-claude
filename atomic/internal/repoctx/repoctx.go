// Package repoctx resolves the working root for the current invocation.
// It prefers the nearest scope="repo" marker in .claude/atomic.toml, then
// calls "git rev-parse --show-toplevel", and falls back to the starting
// directory when neither resolves.
package repoctx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// Resolve returns the absolute path of the working root, resolved from the
// process's own cwd. A thin delegate over ResolveFrom that discards the
// provenance — kept for the two existing production callers (main.go,
// codeintel/cli/realm.go), which need no source information.
func Resolve(override string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repoctx: cannot resolve cwd: %w", err)
	}
	root, _, err := ResolveFrom(cwd, override)
	return root, err
}

// ResolveFrom returns the absolute path of the working root starting from
// dir, plus the mechanism that decided it:
//
//  1. override non-empty → resolved to an absolute path and returned after
//     verifying the directory exists, source ScopeSourceNone (explicit user
//     instruction outranks everything).
//  2. The nearest scope="repo" marker at or above dir (config.FindScopeRoot)
//     → source ScopeSourceMarker.
//  3. "git rev-parse --show-toplevel", run in dir (not the process cwd) →
//     source ScopeSourceGit. Git reporting success with an empty path is
//     treated as no-repo and falls through to step 4.
//  4. dir itself → source ScopeSourceCwd. Git is a history substrate, not a
//     precondition for atomic — commands operate on the starting directory,
//     and the LLM handles saving history separately when a repo exists.
func ResolveFrom(dir, override string) (string, config.ScopeSource, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot resolve path %q: %w", override, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", config.ScopeSourceNone, fmt.Errorf("repoctx: override path does not exist: %q", abs)
		}
		return abs, config.ScopeSourceNone, nil
	}

	if root, found := config.FindScopeRoot(dir, "repo"); found {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot make marker root absolute: %w", err)
		}
		return abs, config.ScopeSourceMarker, nil
	}

	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// Not inside a git repository — fall back to dir rather than failing.
		return resolveDir(dir)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		// git reported success but an empty path — treat as no-repo, fall back.
		return resolveDir(dir)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot make git root absolute: %w", err)
	}
	return abs, config.ScopeSourceGit, nil
}

// resolveDir returns dir made absolute, source cwd — the fallback used when
// neither a marker nor a git repository resolves dir.
func resolveDir(dir string) (string, config.ScopeSource, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot resolve dir %q: %w", dir, err)
	}
	return abs, config.ScopeSourceCwd, nil
}
