package where_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/where"
)

func mkGitMarker(t *testing.T, dir string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git marker: %v", err)
	}
}

// mkGitFileMarker writes ".git" as a regular "gitdir:" pointer file — the
// shape a git worktree uses.
func mkGitFileMarker(t *testing.T, dir string) {
	t.Helper()
	content := "gitdir: /tmp/some-other-place/.git/worktrees/example\n"
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .git file marker: %v", err)
	}
}

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

func mkRealmClaudeMD(t *testing.T, claudeMDPath, wikiIndexPath string) {
	t.Helper()
	content := "<wikis>\n- " + wikiIndexPath + "\n</wikis>\n"
	if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
}

// mkRealmWikiIndex registers memberPaths in a <wiki-scan> block, all indexed.
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

// missingClaudeMD is the standard "no realm registered" input.
func missingClaudeMD(t *testing.T, dir string) string {
	t.Helper()
	return filepath.Join(dir, "does-not-exist", "CLAUDE.md")
}

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

// An unrelated ancestor's docs/wiki/, outside the nearest .git, must not match.
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

// The axes are independent: a realm member may carry its own repo-scope wiki.
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

// A marker nested inside a git repo outranks the (higher-up) git root.
func TestResolveRepoRoot_MarkerWinsOverGit(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	gitRoot := t.TempDir()
	mkGitMarker(t, gitRoot)
	markerRoot := filepath.Join(gitRoot, "server")
	if err := os.MkdirAll(markerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureScopeMarker(markerRoot, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	nested := filepath.Join(markerRoot, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(nested, missingClaudeMD(t, gitRoot))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoRoot.Path != markerRoot {
		t.Errorf("RepoRoot.Path = %q, want marker root %q (not git root %q)", report.RepoRoot.Path, markerRoot, gitRoot)
	}
	if report.RepoRoot.Source != config.ScopeSourceMarker {
		t.Errorf("RepoRoot.Source = %q, want %q", report.RepoRoot.Source, config.ScopeSourceMarker)
	}
}

func TestResolveRepoRoot_GitFallback_NoMarker(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	mkGitMarker(t, root)
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(nested, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoRoot.Path != root {
		t.Errorf("RepoRoot.Path = %q, want git root %q", report.RepoRoot.Path, root)
	}
	if report.RepoRoot.Source != config.ScopeSourceGit {
		t.Errorf("RepoRoot.Source = %q, want %q", report.RepoRoot.Source, config.ScopeSourceGit)
	}
}

func TestResolveRepoRoot_CwdFallback_NoMarkerNoGit(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	dir := t.TempDir()

	report, err := where.Resolve(dir, missingClaudeMD(t, dir))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoRoot.Path != filepath.Clean(dir) {
		t.Errorf("RepoRoot.Path = %q, want cwd %q", report.RepoRoot.Path, dir)
	}
	if report.RepoRoot.Source != config.ScopeSourceCwd {
		t.Errorf("RepoRoot.Source = %q, want %q", report.RepoRoot.Source, config.ScopeSourceCwd)
	}
}

// A worktree's ".git" file must resolve the same as a ".git" directory.
func TestResolveRepoRoot_GitFileMarker_WorktreeStyle(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	mkGitFileMarker(t, root)
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(nested, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RepoRoot.Path != root {
		t.Errorf("RepoRoot.Path = %q, want %q (git-worktree .git file)", report.RepoRoot.Path, root)
	}
	if report.RepoRoot.Source != config.ScopeSourceGit {
		t.Errorf("RepoRoot.Source = %q, want %q", report.RepoRoot.Source, config.ScopeSourceGit)
	}
}

// A marker resolves realm root with no <wikis> registration at all.
func TestResolveRealmScope_MarkerRoot_NoWikisEntry(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}

	report, err := where.Resolve(realmRoot, missingClaudeMD(t, realmRoot))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RealmScope.Position != where.RealmRoot {
		t.Errorf("Position = %v, want RealmRoot", report.RealmScope.Position)
	}
	if report.RealmScope.RealmRoot != realmRoot {
		t.Errorf("RealmRoot = %q, want %q", report.RealmScope.RealmRoot, realmRoot)
	}
	if report.RealmScope.Source != config.ScopeSourceMarker {
		t.Errorf("Source = %q, want %q", report.RealmScope.Source, config.ScopeSourceMarker)
	}
}

func TestResolveRealmScope_MarkerMember(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	mkRealmWikiIndex(t, realmRoot, "repos/alpha")

	memberDir := filepath.Join(realmRoot, "repos", "alpha")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(memberDir, missingClaudeMD(t, realmRoot))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RealmScope.Position != where.RealmMember {
		t.Errorf("Position = %v, want RealmMember", report.RealmScope.Position)
	}
	if report.RealmScope.Source != config.ScopeSourceMarker {
		t.Errorf("Source = %q, want %q", report.RealmScope.Source, config.ScopeSourceMarker)
	}
}

// A realm marked before its first /refresh-wiki has no wiki/index.md yet and
// must degrade to orphaned rather than erroring.
func TestResolveRealmScope_MarkerOrphaned_NoWikiIndexYet(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	sub := filepath.Join(realmRoot, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(sub, missingClaudeMD(t, realmRoot))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RealmScope.Position != where.RealmOrphaned {
		t.Errorf("Position = %v, want RealmOrphaned", report.RealmScope.Position)
	}
	if report.RealmScope.RealmRoot != realmRoot {
		t.Errorf("RealmRoot = %q, want %q", report.RealmScope.RealmRoot, realmRoot)
	}
	if report.RealmScope.Source != config.ScopeSourceMarker {
		t.Errorf("Source = %q, want %q", report.RealmScope.Source, config.ScopeSourceMarker)
	}
}

func TestResolveRealmScope_RegistryFallback_SourceRegistry(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	report, err := where.Resolve(realmRoot, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RealmScope.Position != where.RealmRoot {
		t.Errorf("Position = %v, want RealmRoot", report.RealmScope.Position)
	}
	if report.RealmScope.Source != config.ScopeSourceRegistry {
		t.Errorf("Source = %q, want %q", report.RealmScope.Source, config.ScopeSourceRegistry)
	}
}

func TestResolveRealmScope_NoRealm_SourceNone(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	mkGitMarker(t, root)

	report, err := where.Resolve(root, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RealmScope.Position != where.RealmNone {
		t.Errorf("Position = %v, want RealmNone", report.RealmScope.Position)
	}
	if report.RealmScope.Source != config.ScopeSourceNone {
		t.Errorf("Source = %q, want %q", report.RealmScope.Source, config.ScopeSourceNone)
	}
}

// Marker and registry resolution share classifyRealmPosition so they can never
// classify the same directory differently. Fails if one path is special-cased.
func TestRealmPositionClassification_MarkerAndRegistry_Agree(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")
	memberDir := filepath.Join(realmRoot, "repos", "alpha")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphanDir := filepath.Join(realmRoot, "not-a-member")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cwds := map[string]string{
		"root":     realmRoot,
		"member":   memberDir,
		"orphaned": orphanDir,
	}

	// Round 1: marker path, no <wikis> registration.
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	markerPositions := map[string]where.RealmPosition{}
	for name, cwd := range cwds {
		report, err := where.Resolve(cwd, missingClaudeMD(t, realmRoot))
		if err != nil {
			t.Fatalf("marker-path Resolve(%s): %v", name, err)
		}
		if report.RealmScope.Source != config.ScopeSourceMarker {
			t.Fatalf("marker-path Resolve(%s) source = %q, want marker", name, report.RealmScope.Source)
		}
		markerPositions[name] = report.RealmScope.Position
	}

	// Round 2: same directories, marker removed, resolved via <wikis>.
	if err := os.Remove(config.RepoConfigPath(realmRoot)); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)
	registryPositions := map[string]where.RealmPosition{}
	for name, cwd := range cwds {
		report, err := where.Resolve(cwd, claudeMD)
		if err != nil {
			t.Fatalf("registry-path Resolve(%s): %v", name, err)
		}
		if report.RealmScope.Source != config.ScopeSourceRegistry {
			t.Fatalf("registry-path Resolve(%s) source = %q, want registry", name, report.RealmScope.Source)
		}
		registryPositions[name] = report.RealmScope.Position
	}

	for name := range cwds {
		if markerPositions[name] != registryPositions[name] {
			t.Errorf("%s: marker path = %v, registry path = %v — mechanisms disagree on the same directory", name, markerPositions[name], registryPositions[name])
		}
	}
}

func TestFormatHuman_RepoRootLine(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	if _, err := config.EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}

	report, err := where.Resolve(root, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	human := where.FormatHuman(report)
	want := "repo root:        " + root + " — marker\n"
	if !strings.Contains(human, want) {
		t.Errorf("human output missing repo-root line:\n%s\nwant substring:\n%s", human, want)
	}
}

func TestFormatJSON_RepoRootFields(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	if _, err := config.EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}

	report, err := where.Resolve(root, missingClaudeMD(t, root))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jsonOut, err := where.FormatJSON(report)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}
	if !strings.Contains(jsonOut, `"repo_root"`) {
		t.Errorf("json output missing repo_root field: %s", jsonOut)
	}
	if !strings.Contains(jsonOut, `"source": "marker"`) {
		t.Errorf("json output missing repo_root source: %s", jsonOut)
	}
}

// Only a registry-resolved realm gets the backfill hint; a marker-resolved one
// has nothing to backfill.
func TestFormatHuman_RegistryHint(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	registryReport, err := where.Resolve(realmRoot, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	registryHuman := where.FormatHuman(registryReport)
	if !strings.Contains(registryHuman, "atomic wiki init --scope realm") {
		t.Errorf("expected registry-backfill hint naming `atomic wiki init --scope realm`, got:\n%s", registryHuman)
	}

	restore := config.SetHarnessDirForTest(".claude")
	defer restore()
	markerRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(markerRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	markerReport, err := where.Resolve(markerRoot, missingClaudeMD(t, markerRoot))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	markerHuman := where.FormatHuman(markerReport)
	if strings.Contains(markerHuman, "atomic wiki init --scope realm") {
		t.Errorf("marker-resolved realm should carry no backfill hint, got:\n%s", markerHuman)
	}
}

// The human-only backfill hint stays out of JSON.
func TestFormatJSON_RealmScopeSourceField(t *testing.T) {
	realmRoot := t.TempDir()
	wikiIndexPath := mkRealmWikiIndex(t, realmRoot, "repos/alpha")
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	mkRealmClaudeMD(t, claudeMD, wikiIndexPath)

	report, err := where.Resolve(realmRoot, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jsonOut, err := where.FormatJSON(report)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}
	if !strings.Contains(jsonOut, `"source": "registry"`) {
		t.Errorf("json output missing realm_scope source: %s", jsonOut)
	}
	if strings.Contains(jsonOut, "atomic wiki init") {
		t.Errorf("json output must not carry the human-only backfill hint: %s", jsonOut)
	}
}

// Structural proof of the zero-git-spawn contract: production sources here
// must never import os/exec.
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
