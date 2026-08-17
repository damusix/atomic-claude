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

// Resolve is ResolveFrom against the process cwd, discarding the provenance.
func Resolve(override string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repoctx: cannot resolve cwd: %w", err)
	}
	root, _, err := ResolveFrom(cwd, override)
	return root, err
}

// ResolveFrom returns the absolute working root and the mechanism that decided
// it, trying in order: override, scope="repo" marker, git rev-parse, then dir
// itself. Git is a history substrate, not a precondition, so falling through to
// dir is a normal outcome rather than an error.
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
		return resolveDir(dir)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return resolveDir(dir)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot make git root absolute: %w", err)
	}
	return abs, config.ScopeSourceGit, nil
}

func resolveDir(dir string) (string, config.ScopeSource, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", config.ScopeSourceNone, fmt.Errorf("repoctx: cannot resolve dir %q: %w", dir, err)
	}
	return abs, config.ScopeSourceCwd, nil
}
