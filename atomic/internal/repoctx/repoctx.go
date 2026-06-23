// Package repoctx resolves the working root for the current invocation.
// It calls "git rev-parse --show-toplevel" when no explicit override is given,
// and falls back to the current working directory when not inside a git repo.
package repoctx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve returns the absolute path of the working root.
//
//   - If override is non-empty, it is resolved to an absolute path and returned
//     after verifying the directory exists.
//   - If override is empty, the git toplevel is resolved from the process cwd.
//   - If override is empty and the cwd is not inside a git repository, Resolve
//     falls back to the current working directory. Git is a history substrate,
//     not a precondition for atomic — commands operate on the cwd tree, and the
//     LLM handles saving history separately when a repo exists.
func Resolve(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("repoctx: cannot resolve path %q: %w", override, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("repoctx: override path does not exist: %q", abs)
		}
		return abs, nil
	}

	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		// Not inside a git repository — fall back to the cwd rather than failing.
		return resolveCwd()
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		// git reported success but an empty path — treat as no-repo, fall back.
		return resolveCwd()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("repoctx: cannot make git root absolute: %w", err)
	}
	return abs, nil
}

// resolveCwd returns the absolute current working directory, used as the
// fallback when the invocation is not inside a git repository.
func resolveCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("repoctx: not in a git repo and cannot resolve cwd: %w", err)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("repoctx: cannot make cwd absolute: %w", err)
	}
	return abs, nil
}
