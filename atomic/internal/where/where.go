// Package where implements the `atomic where` orientation verb: it reports a
// cwd's position across four independent axes — repo root (the nearest
// scope="repo" marker in .claude/atomic.toml, else the nearest ancestor
// carrying a .git entry, else cwd), repo-scope wiki presence (docs/wiki/index.md
// found walking up to the nearest .git boundary), realm-scope position
// (none/root/member/orphaned, resolved from the nearest scope="realm" marker
// first and the <wikis> block otherwise), and code-index scope (delegated
// unmodified to codeintel/realm.Resolve). The axes are genuinely orthogonal —
// a repo can be a realm member AND carry its own repo-scope wiki — so Report
// exposes them as independent top-level fields rather than collapsing into
// one enum.
//
// CONTRACT: zero git subprocess spawns. Every detector in this package uses
// only os.Stat / os.ReadFile — no exec.Command — matching the zero-git-spawn
// contract already established in wiki/staleness.go. Repo-root resolution
// therefore diverges from repoctx.ResolveFrom, which runs
// "git rev-parse --show-toplevel" and so understands submodules and GIT_DIR
// overrides that a plain .git stat walk does not. The divergence disappears
// wherever a scope="repo" marker exists — the marker wins in both packages,
// so the .git-vs-git-subprocess difference never gets a chance to matter.
package where

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// RealmPosition identifies cwd's relationship to a registered wiki realm.
type RealmPosition int

const (
	// RealmNone: cwd is not under any registered realm root.
	RealmNone RealmPosition = iota
	// RealmRoot: cwd equals a registered realm root.
	RealmRoot
	// RealmMember: cwd is under a registered member's path within a realm.
	RealmMember
	// RealmOrphaned: cwd is under a realm root but not under any registered
	// member path — a false-positive guard, mirrors realm.ScopeNoIndex's
	// "under root but no matching member" case.
	RealmOrphaned
)

// String renders the position as the lowercase token used in text/JSON output.
func (p RealmPosition) String() string {
	switch p {
	case RealmRoot:
		return "root"
	case RealmMember:
		return "member"
	case RealmOrphaned:
		return "orphaned"
	default:
		return "none"
	}
}

// RepoRootReport describes cwd's repo root and how it was decided.
type RepoRootReport struct {
	// Path is the absolute repo root.
	Path string
	// Source names the mechanism that decided Path: marker, git, or cwd.
	Source config.ScopeSource
}

// RepoScopeReport describes repo-scope wiki detection.
type RepoScopeReport struct {
	// Found is true when docs/wiki/index.md was located walking upward from cwd.
	Found bool
	// Path is the absolute path to the found docs/wiki/index.md. Empty when
	// Found is false.
	Path string
}

// RealmScopeReport describes cwd's position relative to a realm, resolved
// either from a scope="realm" marker or from the <wikis> block.
type RealmScopeReport struct {
	Position RealmPosition
	// RealmRoot is the absolute path to the matched realm root. Empty when
	// Position == RealmNone.
	RealmRoot string
	// Source names the mechanism that decided Position/RealmRoot: marker,
	// registry, or none.
	Source config.ScopeSource
}

// Report composes all four orientation axes for one cwd.
type Report struct {
	RepoRoot   RepoRootReport
	RepoScope  RepoScopeReport
	RealmScope RealmScopeReport
	// CodeIndex is codeintel/realm.Resolve's result, unmodified.
	CodeIndex realm.Resolution
}

// Resolve computes the full orientation report for cwd, reading the <wikis>
// registry from claudeMDPath (conventionally <claudeHome>/CLAUDE.md).
//
// Never calls os.Exit. A non-nil error indicates a genuine I/O failure (e.g.
// claudeMDPath exists but is unreadable) — an absent CLAUDE.md or absent
// <wikis> block is not an error and simply resolves to RealmNone.
func Resolve(cwd, claudeMDPath string) (Report, error) {
	cwd = filepath.Clean(cwd)

	repoRoot := resolveRepoRoot(cwd)
	repoScope := resolveRepoScope(cwd)

	realmScope, err := resolveRealmScope(cwd, claudeMDPath)
	if err != nil {
		return Report{}, err
	}

	codeIndex, err := realm.Resolve(cwd, claudeMDPath)
	if err != nil {
		return Report{}, err
	}

	return Report{
		RepoRoot:   repoRoot,
		RepoScope:  repoScope,
		RealmScope: realmScope,
		CodeIndex:  codeIndex,
	}, nil
}

// resolveRepoRoot decides cwd's repo root: the nearest scope="repo" marker
// at or above cwd, else the nearest ancestor carrying a .git entry (a stat
// walk, not a git subprocess — see the package doc), else cwd itself.
func resolveRepoRoot(cwd string) RepoRootReport {
	if root, found := config.FindScopeRoot(cwd, "repo"); found {
		return RepoRootReport{Path: root, Source: config.ScopeSourceMarker}
	}

	dir := cwd
	for {
		if pathExists(filepath.Join(dir, ".git")) {
			return RepoRootReport{Path: dir, Source: config.ScopeSourceGit}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return RepoRootReport{Path: cwd, Source: config.ScopeSourceCwd}
}

// resolveRepoScope walks upward from cwd checking for docs/wiki/index.md at
// each level, stopping after checking the level where .git is found (a
// directory or, in a worktree, a file) — or at the filesystem root if no
// .git is ever found. Stopping at the git boundary prevents a false-positive
// match against an unrelated ancestor directory's docs/wiki/.
func resolveRepoScope(cwd string) RepoScopeReport {
	dir := cwd
	for {
		candidate := filepath.Join(dir, "docs", "wiki", "index.md")
		if fileExists(candidate) {
			return RepoScopeReport{Found: true, Path: candidate}
		}
		if pathExists(filepath.Join(dir, ".git")) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // filesystem root
		}
		dir = parent
	}
	return RepoScopeReport{}
}

// resolveRealmScope tries the nearest scope="realm" marker at or above cwd
// first (source marker); on a miss it falls back to the pre-existing
// <wikis>-registry path unchanged (source registry), and to RealmNone
// (source none) when neither resolves anything.
func resolveRealmScope(cwd, claudeMDPath string) (RealmScopeReport, error) {
	if report, ok, err := resolveRealmScopeFromMarker(cwd); err != nil {
		return RealmScopeReport{}, err
	} else if ok {
		return report, nil
	}

	return resolveRealmScopeFromRegistry(cwd, claudeMDPath)
}

// resolveRealmScopeFromMarker resolves realm scope from the nearest
// scope="realm" marker at or above cwd. ok is false when no such marker
// exists, in which case the caller falls through to the <wikis> registry.
// Member/orphaned classification, once a marker root is found, proceeds
// through classifyRealmPosition exactly as the registry path does.
func resolveRealmScopeFromMarker(cwd string) (RealmScopeReport, bool, error) {
	realmRoot, found := config.FindScopeRoot(cwd, "realm")
	if !found {
		return RealmScopeReport{}, false, nil
	}

	indexPath := filepath.Join(realmRoot, "wiki", "index.md")
	report, err := classifyRealmPosition(cwd, realmRoot, indexPath, config.ScopeSourceMarker)
	return report, true, err
}

// resolveRealmScopeFromRegistry is the pre-existing <wikis>-block-driven
// resolution — unchanged except every result now carries Source and
// classification is delegated to classifyRealmPosition. Reuses
// wiki.ReadWikiIndexPaths for registered realm roots (the wiki's own
// <wiki-scan> registry — distinct from codeintel/realm's separate code.toml,
// which CodeIndex reads via realm.Resolve unmodified).
func resolveRealmScopeFromRegistry(cwd, claudeMDPath string) (RealmScopeReport, error) {
	indexPaths, err := wiki.ReadWikiIndexPaths(claudeMDPath)
	if err != nil {
		return RealmScopeReport{}, err
	}

	for _, indexPath := range indexPaths {
		// Realm root is the grandparent of wiki/index.md.
		realmRoot := filepath.Clean(filepath.Dir(filepath.Dir(indexPath)))

		if !isUnder(cwd, realmRoot) {
			continue
		}

		return classifyRealmPosition(cwd, realmRoot, indexPath, config.ScopeSourceRegistry)
	}

	return RealmScopeReport{Position: RealmNone, Source: config.ScopeSourceNone}, nil
}

// classifyRealmPosition is the single answer to "where does cwd sit relative
// to this realm root" — shared by the marker and registry resolution paths so
// the two mechanisms can never classify the same directory differently.
// Root when cwd equals realmRoot; member when cwd falls under a path
// registered in indexPath's <wiki-scan> block; orphaned otherwise. This
// naturally covers a realm marked before its first /refresh-wiki: when
// indexPath doesn't exist yet, wiki.ReadScanMembers reports no members rather
// than an error, so an unscanned realm degrades to orphaned instead of
// failing — no separate existence check is needed here.
func classifyRealmPosition(cwd, realmRoot, indexPath string, source config.ScopeSource) (RealmScopeReport, error) {
	if cwd == realmRoot {
		return RealmScopeReport{Position: RealmRoot, RealmRoot: realmRoot, Source: source}, nil
	}

	members, err := wiki.ReadScanMembers(indexPath)
	if err != nil {
		return RealmScopeReport{}, err
	}
	for _, m := range members {
		memberAbs := filepath.Clean(filepath.Join(realmRoot, m.Path))
		if isUnder(cwd, memberAbs) {
			return RealmScopeReport{Position: RealmMember, RealmRoot: realmRoot, Source: source}, nil
		}
	}

	return RealmScopeReport{Position: RealmOrphaned, RealmRoot: realmRoot, Source: source}, nil
}

// isUnder reports whether child is equal to or under parent, using normalized
// path-prefix comparison (no symlink resolution). A private copy — see
// wiki.isUnder and codeintel/realm.isUnder for the two prior copies; a third
// is accepted per this feature's design non-goals rather than forcing a
// cross-package extraction unrelated to this change.
func isUnder(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// fileExists returns true when path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pathExists returns true when path exists, regardless of type (used for the
// .git boundary check, which may be a directory or, in a worktree, a file).
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
