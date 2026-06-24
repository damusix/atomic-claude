package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitToplevelFn is the resolver used by all production code to locate the git
// repository root. Tests may replace this variable with a counting or fake
// resolver to verify call-count invariants.
var gitToplevelFn = gitToplevel

// IsRepoDev reports whether cwd is inside the atomic-claude repo. The
// heuristic: locate the git toplevel from cwd, then check whether
// atomic/internal/bundlemirror/mirror.go exists relative to that root. If git
// is unavailable or cwd is not in a repo, cwd itself is used as the candidate
// root.
//
// When false, category 5 (manifest) should be skipped.
func IsRepoDev(cwd string) (bool, error) {
	root := gitToplevelFn(cwd)
	return isRepoDevRoot(root)
}

// isRepoDevRoot checks the marker file at an already-resolved repo root without
// spawning git. Use this when the toplevel has already been computed.
func isRepoDevRoot(root string) (bool, error) {
	marker := filepath.Join(root, "atomic", "internal", "bundlemirror", "mirror.go")
	_, err := os.Stat(marker)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// gitToplevel runs `git rev-parse --show-toplevel` in cwd and returns the
// result. Falls back to cwd on error (git absent, not a repo, etc.).
func gitToplevel(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return cwd
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return cwd
	}
	return top
}
