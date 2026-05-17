// Package repoctx resolves the repository root for the current invocation.
// It calls "git rev-parse --show-toplevel" when no explicit override is given.
package repoctx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve returns the absolute path of the repository root.
//
//   - If override is non-empty, it is resolved to an absolute path and returned
//     after verifying the directory exists.
//   - If override is empty, the git toplevel is resolved from the process cwd.
//     Returns an error when not inside a git repository.
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
		return "", fmt.Errorf("repoctx: not inside a git repository (git rev-parse failed): %w", err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("repoctx: git rev-parse returned empty path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("repoctx: cannot make git root absolute: %w", err)
	}
	return abs, nil
}
