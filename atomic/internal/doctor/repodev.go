package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitToplevelFn is the repo-root resolver all production code goes through.
// Tests swap in a counting or fake resolver to assert call-count invariants.
var gitToplevelFn = gitToplevel

// IsRepoDev reports whether cwd is inside the atomic-claude repo, detected by
// the presence of atomic/internal/bundlemirror/mirror.go at the git toplevel.
// Category 5 (manifest) is skipped when this is false.
func IsRepoDev(cwd string) (bool, error) {
	root := gitToplevelFn(cwd)
	return isRepoDevRoot(root)
}

// isRepoDevRoot checks the marker at an already-resolved root, without git.
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

// gitToplevel falls back to cwd when git is absent or cwd is not a repo.
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
