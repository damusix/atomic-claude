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

// repoDev reports whether root is the atomic-claude development repo, detected
// by the presence of the bundle-mirror source that only exists in-repo. The
// bundle-parity check compares the working tree against the embedded source
// snapshot, which has no meaning outside this repo, so callers skip it when
// this returns false. Mirrors doctor.IsRepoDev's marker heuristic without
// importing the doctor package.
func repoDev(root string) bool {
	if root == "" {
		return false
	}
	marker := filepath.Join(root, "atomic", "internal", "bundlemirror", "mirror.go")
	_, err := os.Stat(marker)
	return err == nil
}
