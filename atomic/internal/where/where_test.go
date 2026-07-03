package where_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/where"
)

// mkGitMarker creates a ".git" directory marker under dir — a pure filesystem
// stat target, no real git repository required.
func mkGitMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git marker: %v", err)
	}
}

// mkRepoScopeWiki writes docs/wiki/index.md under dir (content is irrelevant
// to detection — only existence matters).
func mkRepoScopeWiki(t *testing.T, dir string) string {
	t.Helper()
	wikiDir := filepath.Join(dir, "docs", "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdir docs/wiki: %v", err)
	}
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte("# repo wiki\n"), 0o644); err != nil {
		t.Fatalf("write docs/wiki/index.md: %v", err)
	}
	return indexPath
}

// mkRealmClaudeMD writes a CLAUDE.md at claudeMDPath registering a single
// realm whose wiki index.md lives at realmRoot/wiki/index.md.
func mkRealmClaudeMD(t *testing.T, claudeMDPath, wikiIndexPath string) {
	t.Helper()
	content := "<wikis>\n- " + wikiIndexPath + "\n</wikis>\n"
	if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
}

// mkRealmWikiIndex writes realmRoot/wiki/index.md with a <wiki-scan> block
// registering the given member paths (all status="indexed").
func mkRealmWikiIndex(t *testing.T, realmRoot string, memberPaths ...string) string {
	t.Helper()
	wikiDir := filepath.Join(realmRoot, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdir realm wiki dir: %v", err)
	}
	var sb strings.Builder
	sb.WriteString("<wiki-scan generated=\"2026-01-01\" root=\"" + realmRoot + "\">\n")
	for _, p := range memberPaths {
		sb.WriteString(`  <repo path="` + p + `" status="indexed"/>` + "\n")
	}
	sb.WriteString("</wiki-scan>\n")
	indexPath := filepath.Join(wikiDir, "index.md")
	if err := os.WriteFile(indexPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write realm wiki index.md: %v", err)
	}
	return indexPath
}

// missingClaudeMD returns a path to a CLAUDE.md that does not exist — the
// standard "no realm registered" input.
func missingClaudeMD(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, "does-not-exist", "CLAUDE.md")
}

// TestResolve_PlainRepo_AllAxesAbsent covers SC1: a plain repo with no
// docs/wiki/, no realm registration, and no code index reports all three
// axes as absent/none.
func TestResolve_PlainRepo_AllAxesAbsent(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)

	report, err := where.Resolve(root, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoScope.Found {
		t.Errorf("expected RepoScope.Found=false, got true (path=%q)", report.RepoScope.Path)
	}
	if report.RealmScope.Position != where.RealmNone {
		t.Errorf("expected RealmNone, got %v", report.RealmScope.Position)
	}
	if report.CodeIndex.Scope != realm.ScopeNoIndex {
		t.Errorf("expected ScopeNoIndex, got %v", report.CodeIndex.Scope)
	}
}

// TestResolve_RepoScopeWikiFound_FromNestedCwd covers SC2: docs/wiki/index.md
// present at an ancestor (up to the .git boundary) is found from a nested cwd.
func TestResolve_RepoScopeWikiFound_FromNestedCwd(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	wantPath := mkRepoScopeWiki(t, root)

	nested := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(nested, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.RepoScope.Found {
		t.Fatal("expected RepoScope.Found=true")
	}
	if report.RepoScope.Path != wantPath {
		t.Errorf("path mismatch:\n  got:  %s\n  want: %s", report.RepoScope.Path, wantPath)
	}
}

// TestResolve_RepoScopeWiki_StopsAtGitBoundary covers the false-positive-guard
// risk noted in the spec: an unrelated ancestor's docs/wiki/ (outside the
// nearest .git) must NOT be reported.
func TestResolve_RepoScopeWiki_StopsAtGitBoundary(t *testing.T) {
	outer := t.TempDir()
	mkRepoScopeWiki(t, outer) // ancestor wiki — must be invisible

	root := filepath.Join(outer, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	mkGitMarker(t, root) // .git boundary — walk must stop here

	report, err := where.Resolve(root, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoScope.Found {
		t.Errorf("expected RepoScope.Found=false (ancestor wiki outside .git boundary), got path=%q", report.RepoScope.Path)
	}
}

// TestResolve_RealmScope_Root_Member_Orphaned covers SC3: root/member/orphaned
// classification against one registered realm.
func TestResolve_RealmScope_Root_Member_Orphaned(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")

	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	memberDir := filepath.Join(realmRoot, "repos", "alpha")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanDir := filepath.Join(realmRoot, "not-a-member")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("root", func(t *testing.T) {
		report, err := where.Resolve(realmRoot, claudeMD)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.RealmScope.Position != where.RealmRoot {
			t.Errorf("expected RealmRoot, got %v", report.RealmScope.Position)
		}
		if report.RealmScope.RealmRoot != filepath.Clean(realmRoot) {
			t.Errorf("realm root mismatch: got %q want %q", report.RealmScope.RealmRoot, realmRoot)
		}
	})

	t.Run("member", func(t *testing.T) {
		report, err := where.Resolve(memberDir, claudeMD)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.RealmScope.Position != where.RealmMember {
			t.Errorf("expected RealmMember, got %v", report.RealmScope.Position)
		}
	})

	t.Run("orphaned", func(t *testing.T) {
		report, err := where.Resolve(orphanDir, claudeMD)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.RealmScope.Position != where.RealmOrphaned {
			t.Errorf("expected RealmOrphaned, got %v", report.RealmScope.Position)
		}
	})

	t.Run("none-outside-realm", func(t *testing.T) {
		outside := t.TempDir()
		report, err := where.Resolve(outside, claudeMD)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.RealmScope.Position != where.RealmNone {
			t.Errorf("expected RealmNone, got %v", report.RealmScope.Position)
		}
	})
}

// TestResolve_Composite_RealmMemberWithOwnRepoScopeWiki covers SC4: a cwd
// that is simultaneously a realm member AND carries its own repo-scope
// docs/wiki/index.md reports both facts together in one call.
func TestResolve_Composite_RealmMemberWithOwnRepoScopeWiki(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")

	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	memberRoot := filepath.Join(realmRoot, "repos", "alpha")
	if err := os.MkdirAll(memberRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mkGitMarker(t, memberRoot)
	ownWikiPath := mkRepoScopeWiki(t, memberRoot)

	nested := filepath.Join(memberRoot, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(nested, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !report.RepoScope.Found {
		t.Error("expected RepoScope.Found=true (composite: own repo-scope wiki)")
	}
	if report.RepoScope.Path != ownWikiPath {
		t.Errorf("repo-scope path mismatch: got %q want %q", report.RepoScope.Path, ownWikiPath)
	}
	if report.RealmScope.Position != where.RealmMember {
		t.Errorf("expected RealmMember (composite), got %v", report.RealmScope.Position)
	}
}

// TestResolve_CodeIndexScope_PassThroughUnmodified covers SC5: code-index
// scope reflects codeintel/realm.Resolve's result unmodified and appears
// unconditionally, regardless of --json.
func TestResolve_CodeIndexScope_PassThroughUnmodified(t *testing.T) {
	root := t.TempDir()
	mkGitMarker(t, root)
	dbDir := filepath.Join(root, ".claude", ".atomic-index")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "atomic.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	claudeMD := missingClaudeMD(t, root)

	directResolution, err := realm.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error from realm.Resolve: %v", err)
	}

	report, err := where.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.CodeIndex.Scope != directResolution.Scope {
		t.Errorf("CodeIndex.Scope diverges from realm.Resolve: got %v want %v", report.CodeIndex.Scope, directResolution.Scope)
	}
	if report.CodeIndex.Scope != realm.ScopeRepo {
		t.Errorf("expected ScopeRepo, got %v", report.CodeIndex.Scope)
	}
}

// TestFormatJSON_CarriesSameInformationAsHuman covers SC6: --json emits the
// same information as the plain-text report.
func TestFormatJSON_CarriesSameInformationAsHuman(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	report, err := where.Resolve(realmRoot, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	human := where.FormatHuman(report)
	jsonOut, err := where.FormatJSON(report)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}

	if !strings.Contains(human, "root") {
		t.Errorf("human output missing realm root position: %s", human)
	}
	if !strings.Contains(jsonOut, `"position": "root"`) {
		t.Errorf("json output missing realm root position: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"realm_root"`) {
		t.Errorf("json output missing realm_root field: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"code_index"`) {
		t.Errorf("json output missing code_index field (must be unconditional, not flag-gated): %s", jsonOut)
	}
}

// TestZeroGitSpawns_NoOSExecImport is a structural proof of the zero-git-spawn
// contract: the where package's production source files must never import
// os/exec. Complements wiki/staleness.go's runtime-injected-runner proof —
// this package has no exec dependency to inject in the first place.
func TestZeroGitSpawns_NoOSExecImport(t *testing.T) {
	for _, name := range []string{"where.go", "format.go"} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			if imp.Path.Value == `"os/exec"` {
				t.Errorf("%s imports os/exec — violates zero-git-spawn contract", name)
			}
		}
	}
}
