// The directory-skip predicate every walker in this package shares.
package serve

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// shouldSkipDir skips dotdirs and build dumps except .claude, whose markdown
// wiki links cite (docs/spec/atomic-serve.md). hiddenDir exempts state roots.
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
	base           string          // walk root exactly as the walker phrases its paths
	realBase       string          // same directory with symlinks and relativity resolved
	loaded         map[string]bool // repo roots already queried
	paths          map[string]bool // resolved paths git reports as ignored
	stateRoots     map[string]bool // resolved repository-state roots of the repos seen
	stateAncestors map[string]bool // dot-led directories a nested state root sits under
}

// newGitIgnores loads the ignore set of the repository enclosing root. Outside
// a repository the set is empty and the walk is unchanged.
func newGitIgnores(root string) *gitIgnores {
	g := emptyGitIgnores(root)
	if top, err := gitToplevel(root); err == nil {
		g.load(resolveDir(top))
	} else {
		g.exemptStateRoot(g.realBase)
	}
	return g
}

// newStateRoots is newGitIgnores without the ignore listing, for a caller that
// reads only hiddenDir.
func newStateRoots(root string) *gitIgnores {
	g := emptyGitIgnores(root)
	repo := g.realBase
	if !isGitRepoRoot(repo) {
		if top, err := gitToplevel(root); err == nil {
			repo = resolveDir(top)
		}
	}
	g.exemptStateRoot(repo)
	return g
}

func emptyGitIgnores(root string) *gitIgnores {
	return &gitIgnores{
		base:           root,
		realBase:       resolveDir(root),
		loaded:         map[string]bool{},
		paths:          map[string]bool{},
		stateRoots:     map[string]bool{},
		stateAncestors: map[string]bool{},
	}
}

// exemptStateRoot records repoRoot's state root so the dot rule does not hide
// it. A root outside repoRoot, or one the ladder refuses, is never walked.
func (g *gitIgnores) exemptStateRoot(repoRoot string) {
	dir, ok := config.QuietStateDir(repoRoot)
	if !ok {
		return
	}
	rel, ok := config.RepoRelative(repoRoot, dir)
	if !ok || rel == "." {
		return
	}
	g.stateRoots[filepath.Join(repoRoot, rel)] = true
	parts := strings.Split(rel, string(filepath.Separator))
	fenced := false
	for i := 0; i < len(parts)-1; i++ {
		fenced = fenced || shouldSkipDir(parts[i])
		if fenced {
			g.stateAncestors[filepath.Join(append([]string{repoRoot}, parts[:i+1]...)...)] = true
		}
	}
}

// skipWalkDir is the test a walker applies below its root.
func (g *gitIgnores) skipWalkDir(dir string) bool {
	return g.hiddenDir(dir) || g.skipDir(dir)
}

// hiddenDir is shouldSkipDir with state roots exempt. Inside a skipped ancestor
// of a nested state root, only the path toward that root shows.
func (g *gitIgnores) hiddenDir(dir string) bool {
	real := g.resolve(dir)
	if g.stateRoots[real] || g.stateAncestors[real] {
		return false
	}
	return g.stateAncestors[filepath.Dir(real)] || shouldSkipDir(filepath.Base(dir))
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
	g.exemptStateRoot(repoRoot)

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
		real := g.resolve(dir)
		if g.worktreeOfLoaded(real) {
			return true
		}
		g.load(real)
		return false
	}
	return g.paths[g.resolve(dir)]
}

func (g *gitIgnores) skipFile(path string) bool {
	real := g.resolve(path)
	return g.paths[real] || g.stateAncestors[filepath.Dir(real)]
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

// isGitRepoRoot reports whether dir is a checkout: .git is a directory, or the
// file a linked worktree carries.
func isGitRepoRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// worktreeOfLoaded: dir is a second copy when nested in a loaded checkout of
// the same repository; containment keeps sibling members walk-order independent.
func (g *gitIgnores) worktreeOfLoaded(dir string) bool {
	common, ok := gitCommonDir(dir)
	if !ok {
		return false
	}
	for repo := range g.loaded {
		if !strings.HasPrefix(dir, repo+string(filepath.Separator)) {
			continue
		}
		if c, ok := gitCommonDir(repo); ok && c == common {
			return true
		}
	}
	return false
}

// gitCommonDir resolves as git does: only a linked worktree's gitdir holds a
// commondir file, so a submodule's gitdir is its own common dir.
func gitCommonDir(dir string) (string, bool) {
	dotgit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotgit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return resolveDir(dotgit), true
	}
	data, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return "", false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	if c, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitdir, common)
		}
		return resolveDir(common), true
	}
	return resolveDir(gitdir), true
}
