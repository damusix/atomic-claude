// The directory-skip predicate every walker in this package shares.
package serve

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// shouldSkipDir skips dot-directories and the usual build dumps, with one
// exception: .claude itself holds servable project docs that wiki links cite
// across members, so skipping it would render valid links broken. Dotdirs
// nested inside it are still skipped by the leading-dot rule.
//
// Callers apply this only to sub-directories, never to the root.
func shouldSkipDir(name string) bool {
	if name == ".claude" {
		return false
	}
	if len(name) > 0 && name[0] == '.' {
		return true
	}
	switch name {
	case "node_modules", "vendor", "tmp":
		return true
	}
	return false
}

// hiddenFile marks the backups, caches, and machinery no walker enumerates.
// Only files reach this; .claude is a directory, handled by shouldSkipDir.
func hiddenFile(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// gitIgnores excludes git-ignored paths from a walk spanning several repos.
//
// Ignored is not the test on its own: a realm gitignores its member repos, and
// those are exactly what it exists to serve. The test is whether a directory is
// its own repository. Enumeration only — an ignored file still renders by path.
type gitIgnores struct {
	base     string          // walk root exactly as the walker phrases its paths
	realBase string          // same directory with symlinks and relativity resolved
	loaded   map[string]bool // repo roots already queried
	paths    map[string]bool // resolved paths git reports as ignored
}

// newGitIgnores loads the ignore set of the repository enclosing root. Outside
// a repository the set is empty and the walk is unchanged.
func newGitIgnores(root string) *gitIgnores {
	g := &gitIgnores{
		base:     root,
		realBase: resolveDir(root),
		loaded:   map[string]bool{},
		paths:    map[string]bool{},
	}
	if top, err := gitToplevel(root); err == nil {
		g.load(resolveDir(top))
	}
	return g
}

// resolve rewrites a walker path into the form load stores. WalkDir echoes the
// root it was handed while git always answers with the real absolute path, so a
// relative or symlinked root makes every lookup silently miss.
func (g *gitIgnores) resolve(path string) string {
	if g.base == "" || g.base == g.realBase {
		return path
	}
	rel, err := filepath.Rel(g.base, path)
	if err != nil {
		return path
	}
	return filepath.Join(g.realBase, rel)
}

// resolveDir gives dir's absolute, symlink-free form.
func resolveDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return real
}

// load records every path git ignores under repoRoot. --directory collapses a
// wholly-ignored directory to one entry, keeping the set small.
func (g *gitIgnores) load(repoRoot string) {
	if repoRoot == "" || g.loaded[repoRoot] {
		return
	}
	g.loaded[repoRoot] = true

	// -z because git C-quotes non-ASCII paths, and a quoted string never matches.
	cmd := exec.Command("git", "ls-files", "-z", "--directory", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return // not a repo, or git unavailable: nothing to exclude
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		rel := string(entry)
		if rel == "" {
			continue
		}
		g.paths[filepath.Join(repoRoot, rel)] = true
	}
}

// skipDir reports whether the walk should stop at dir.
func (g *gitIgnores) skipDir(dir string) bool {
	// Checked before ignored-ness: a realm that does not gitignore its members
	// would otherwise walk them under the parent's rules, which say nothing
	// about the member's own worktrees or build output.
	if isGitRepoRoot(dir) {
		g.load(g.resolve(dir))
		return false
	}
	return g.paths[g.resolve(dir)]
}

func (g *gitIgnores) skipFile(path string) bool {
	return g.paths[g.resolve(path)]
}

// gitToplevel resolves the repository root containing dir.
func gitToplevel(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isGitRepoRoot reports whether dir is a distinct repository. A linked worktree
// carries .git as a file pointing at the repo it duplicates, so only a
// directory counts.
func isGitRepoRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}
