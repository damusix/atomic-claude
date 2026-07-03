// Package where implements the `atomic where` orientation verb: it reports a
// cwd's position across three independent axes — repo-scope wiki presence
// (docs/wiki/index.md found walking up to the nearest .git boundary),
// realm-scope position (none/root/member/orphaned, relative to any
// <wikis>-registered realm), and code-index scope (delegated unmodified to
// codeintel/realm.Resolve). The three axes are genuinely orthogonal — a repo
// can be a realm member AND carry its own repo-scope wiki — so Report exposes
// them as independent top-level fields rather than collapsing into one enum.
//
// CONTRACT: zero git subprocess spawns. Every detector in this package uses
// only os.Stat / os.ReadFile — no exec.Command — matching the zero-git-spawn
// contract already established in wiki/staleness.go.
package where

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
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

// RepoScopeReport describes repo-scope wiki detection.
type RepoScopeReport struct {
	// Found is true when docs/wiki/index.md was located walking upward from cwd.
	Found bool
	// Path is the absolute path to the found docs/wiki/index.md. Empty when
	// Found is false.
	Path string
}

// RealmScopeReport describes cwd's position relative to any realm registered
// in the <wikis> block.
type RealmScopeReport struct {
	Position RealmPosition
	// RealmRoot is the absolute path to the matched realm root. Empty when
	// Position == RealmNone.
	RealmRoot string
}

// Report composes all three orientation axes for one cwd.
type Report struct {
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
		RepoScope:  repoScope,
		RealmScope: realmScope,
		CodeIndex:  codeIndex,
	}, nil
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

// resolveRealmScope reuses wiki.ReadWikiIndexPaths for registered realm roots
// and wiki.ReadScanMembers for each realm's registered member paths (the
// wiki's own <wiki-scan> registry — distinct from codeintel/realm's separate
// code.toml, which CodeIndex reads via realm.Resolve unmodified).
func resolveRealmScope(cwd, claudeMDPath string) (RealmScopeReport, error) {
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

		if cwd == realmRoot {
			return RealmScopeReport{Position: RealmRoot, RealmRoot: realmRoot}, nil
		}

		members, err := wiki.ReadScanMembers(indexPath)
		if err != nil {
			return RealmScopeReport{}, err
		}
		for _, m := range members {
			memberAbs := filepath.Clean(filepath.Join(realmRoot, m.Path))
			if isUnder(cwd, memberAbs) {
				return RealmScopeReport{Position: RealmMember, RealmRoot: realmRoot}, nil
			}
		}

		// Under the realm root but not under any registered member path.
		return RealmScopeReport{Position: RealmOrphaned, RealmRoot: realmRoot}, nil
	}

	return RealmScopeReport{Position: RealmNone}, nil
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
