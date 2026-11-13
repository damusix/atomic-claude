// Package where implements the `atomic where` orientation verb, reporting a
// cwd's position across four orthogonal axes: repo root, repo-scope wiki,
// realm scope, and code-index scope.
//
// Zero git subprocess spawns: every detector uses os.Stat / os.ReadFile only.
// Repo-root resolution therefore diverges from repoctx.ResolveFrom, which
// shells out to git and so understands submodules and GIT_DIR overrides a
// plain .git stat walk does not. A scope="repo" marker wins in both packages,
// so the divergence only surfaces where no marker exists.
package where

import (
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// RealmPosition identifies cwd's relationship to a registered wiki realm.
type RealmPosition int

const (
	RealmNone RealmPosition = iota
	RealmRoot
	RealmMember
	// RealmOrphaned: under a realm root but under no registered member path.
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
	Path   string
	Source config.ScopeSource
}

// RepoScopeReport describes repo-scope wiki detection. Path is the located
// docs/wiki/index.md, empty when Found is false.
type RepoScopeReport struct {
	Found bool
	Path  string
}

// RealmScopeReport describes cwd's position relative to a realm. RealmRoot is
// empty when Position is RealmNone.
type RealmScopeReport struct {
	Position  RealmPosition
	RealmRoot string
	Source    config.ScopeSource
}

// Report composes all four orientation axes for one cwd.
type Report struct {
	RepoRoot   RepoRootReport
	RepoScope  RepoScopeReport
	RealmScope RealmScopeReport
	CodeIndex  realm.Resolution
}

// Resolve computes the orientation report for cwd from ~/.atomic/wikis.md, else the
// <wikis> block at claudeMDPath. An absent registry resolves to RealmNone, not an error.
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

// resolveRepoRoot: nearest scope="repo" marker, else nearest .git ancestor,
// else cwd.
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

// resolveRepoScope walks upward for docs/wiki/index.md, stopping at the .git
// boundary so an unrelated ancestor's docs/wiki/ cannot false-positive.
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

// resolveRealmScope prefers the nearest scope="realm" marker, falling back to
// the <wikis> registry.
func resolveRealmScope(cwd, claudeMDPath string) (RealmScopeReport, error) {
	loc, found, err := wiki.FindRealm(cwd, claudeMDPath)
	if err != nil {
		return RealmScopeReport{}, err
	}
	if !found {
		return RealmScopeReport{Position: RealmNone, Source: config.ScopeSourceNone}, nil
	}
	return classifyRealmPosition(cwd, loc.Root, loc.IndexPath, loc.Source)
}

// classifyRealmPosition is shared by the marker and registry paths so the two
// can never classify the same directory differently. A realm marked before its
// first /refresh-wiki needs no existence check: ReadScanMembers reports no
// members rather than an error, so an unscanned realm degrades to orphaned.
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
		if wiki.IsUnder(cwd, memberAbs) {
			return RealmScopeReport{Position: RealmMember, RealmRoot: realmRoot, Source: source}, nil
		}
	}

	return RealmScopeReport{Position: RealmOrphaned, RealmRoot: realmRoot, Source: source}, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pathExists ignores file type: .git is a directory in a normal clone and a
// file in a worktree.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
