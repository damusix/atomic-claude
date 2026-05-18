package validate

import (
	"os"
	"path/filepath"
)

// findRepoRoot walks up from startDir looking for a .git entry (file or
// directory). Returns the directory containing .git or an empty string if not
// found. The .git-as-file case handles git worktrees where .git is a regular
// file containing "gitdir: ..." rather than a directory.
//
// This is a minimal implementation for CP-3. A richer version (tolerating
// additional VCS markers, configurable stop path) ships in CP-6.
func findRepoRoot(startDir string) string {
	dir := startDir
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding .git.
			return ""
		}
		dir = parent
	}
}
