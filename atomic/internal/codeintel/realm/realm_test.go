package realm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// writeFile writes content to path, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// buildClaudeMD writes a CLAUDE.md with a <wikis> block listing the given
// wiki/index.md paths.
func buildClaudeMD(t *testing.T, claudeMDPath string, wikiIndexPaths []string) {
	t.Helper()
	block := "<wikis>\n"
	for _, p := range wikiIndexPaths {
		block += "- " + p + "\n"
	}
	block += "</wikis>\n"
	writeFile(t, claudeMDPath, "# CLAUDE.md\n\n"+block)
}

// ─── Config (code.toml) tests ────────────────────────────────────────────────

func TestLoadConfig_HappyPath(t *testing.T) {
	dir := t.TempDir()
	toml := `[[member]]
key = "alpha"
path = "repos/alpha"
exclude = false

[[member]]
key = "beta"
path = "repos/beta"
exclude = true
`
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), toml)

	cfg, err := realm.LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config for existing file")
	}
	if len(cfg.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(cfg.Members))
	}
	if cfg.Members[0].Key != "alpha" {
		t.Errorf("expected key=alpha, got %q", cfg.Members[0].Key)
	}
	if cfg.Members[0].Path != "repos/alpha" {
		t.Errorf("expected path=repos/alpha, got %q", cfg.Members[0].Path)
	}
	if cfg.Members[0].Exclude {
		t.Error("expected exclude=false for alpha")
	}
	if cfg.Members[1].Key != "beta" {
		t.Errorf("expected key=beta, got %q", cfg.Members[1].Key)
	}
	if !cfg.Members[1].Exclude {
		t.Error("expected exclude=true for beta")
	}
}

func TestLoadConfig_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	// No .atomic/code.toml written.

	cfg, err := realm.LoadConfig(dir)
	if err != nil {
		t.Fatalf("absent file must not error, got: %v", err)
	}
	if cfg != nil {
		t.Fatal("absent file must return nil Config (not-yet-seeded sentinel)")
	}
}

func TestLoadConfig_ExcludeMember(t *testing.T) {
	dir := t.TempDir()
	toml := `[[member]]
key = "pending-repo"
path = "trash/pending-repo"
exclude = true
`
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), toml)

	cfg, err := realm.LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Members) != 1 {
		t.Fatalf("expected 1 member, got %v", cfg)
	}
	if !cfg.Members[0].Exclude {
		t.Error("expected exclude=true")
	}
}

// ─── Resolver tests ──────────────────────────────────────────────────────────

// makeRealm sets up a realm at realmDir with:
//   - wiki/index.md (registered in claudeMD)
//   - .atomic/code.toml listing the given members
//   - member dirs created on disk
func makeRealm(t *testing.T, realmDir, claudeMDPath string, members []realm.MemberEntry) {
	t.Helper()
	// Write wiki/index.md so the path exists.
	writeFile(t, filepath.Join(realmDir, "wiki", "index.md"), "# wiki\n")
	// Register in CLAUDE.md.
	buildClaudeMD(t, claudeMDPath, []string{filepath.Join(realmDir, "wiki", "index.md")})
	// Write code.toml if members provided.
	if len(members) > 0 {
		toml := ""
		for _, m := range members {
			excl := "false"
			if m.Exclude {
				excl = "true"
			}
			toml += "[[member]]\nkey = \"" + m.Key + "\"\npath = \"" + m.Path + "\"\nexclude = " + excl + "\n\n"
		}
		writeFile(t, filepath.Join(realmDir, ".atomic", "code.toml"), toml)
	}
	// Create member directories on disk.
	for _, m := range members {
		abs := filepath.Join(realmDir, m.Path)
		if err := os.MkdirAll(abs, 0o755); err != nil {
			t.Fatalf("mkdir member %s: %v", abs, err)
		}
	}
}

// TestResolve_LocalIndexPresent verifies Repo scope when a local .atomic-index/atomic.db exists.
func TestResolve_LocalIndexPresent(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, claudeMD, "# no wikis\n")

	// Place a local index db.
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")
	writeFile(t, dbPath, "")

	res, err := realm.Resolve(dir, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeRepo {
		t.Errorf("expected ScopeRepo, got %v", res.Scope)
	}
}

// TestResolve_RealmAll verifies RealmAll scope when cwd == realm root.
func TestResolve_RealmAll(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	members := []realm.MemberEntry{
		{Key: "foo", Path: "repos/foo"},
		{Key: "bar", Path: "repos/bar"},
	}
	makeRealm(t, root, claudeMD, members)

	// cwd == realm root, no local index.
	res, err := realm.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeRealmAll {
		t.Errorf("expected ScopeRealmAll, got %v", res.Scope)
	}
	if res.RealmRoot != root {
		t.Errorf("expected RealmRoot=%q, got %q", root, res.RealmRoot)
	}
	if res.Config == nil {
		t.Fatal("expected non-nil Config")
	}
	if len(res.Config.Members) != 2 {
		t.Errorf("expected 2 members, got %d", len(res.Config.Members))
	}
	// res.Members is the non-excluded slice CP3 will fan out across.
	// Both fixture members have exclude=false, so all 2 must appear.
	if len(res.Members) != 2 {
		t.Errorf("expected 2 non-excluded members in res.Members, got %d", len(res.Members))
	}
}

// TestResolve_RealmMember verifies RealmMember scope when cwd is inside a member path.
func TestResolve_RealmMember(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	members := []realm.MemberEntry{
		{Key: "foo", Path: "repos/foo"},
		{Key: "bar", Path: "repos/bar"},
	}
	makeRealm(t, root, claudeMD, members)

	// cwd inside member foo.
	cwdInFoo := filepath.Join(root, "repos", "foo", "src")
	if err := os.MkdirAll(cwdInFoo, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := realm.Resolve(cwdInFoo, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeRealmMember {
		t.Errorf("expected ScopeRealmMember, got %v", res.Scope)
	}
	if res.RealmRoot != root {
		t.Errorf("expected RealmRoot=%q, got %q", root, res.RealmRoot)
	}
	if len(res.Members) != 1 {
		t.Fatalf("expected 1 resolved member, got %d", len(res.Members))
	}
	if res.Members[0].Key != "foo" {
		t.Errorf("expected member key=foo, got %q", res.Members[0].Key)
	}
}

// TestResolve_RealmRootButNonMemberSubdir verifies NoIndex when cwd is inside
// the realm root but not under any member path (e.g. wiki/).
func TestResolve_RealmRootButNonMemberSubdir(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	members := []realm.MemberEntry{
		{Key: "foo", Path: "repos/foo"},
	}
	makeRealm(t, root, claudeMD, members)

	// cwd inside wiki/ — a realm subdir but not a member.
	cwdInWiki := filepath.Join(root, "wiki")

	res, err := realm.Resolve(cwdInWiki, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeNoIndex {
		t.Errorf("expected ScopeNoIndex for non-member subdir, got %v", res.Scope)
	}
}

// TestResolve_OutsideAnyRealm verifies NoIndex when cwd is outside any registered realm.
func TestResolve_OutsideAnyRealm(t *testing.T) {
	// Two separate temp dirs — one is the realm, the other is cwd.
	realmDir := t.TempDir()
	outsideDir := t.TempDir()
	claudeMD := filepath.Join(realmDir, "CLAUDE.md")

	members := []realm.MemberEntry{
		{Key: "foo", Path: "repos/foo"},
	}
	makeRealm(t, realmDir, claudeMD, members)

	res, err := realm.Resolve(outsideDir, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeNoIndex {
		t.Errorf("expected ScopeNoIndex for outside-realm cwd, got %v", res.Scope)
	}
}

// TestResolve_NoWikisRegistered verifies NoIndex when CLAUDE.md has no <wikis> block.
func TestResolve_NoWikisRegistered(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, claudeMD, "# no wikis\n")

	res, err := realm.Resolve(dir, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeNoIndex {
		t.Errorf("expected ScopeNoIndex, got %v", res.Scope)
	}
}

// TestResolve_WikiRegistryReadError verifies that a hard I/O error from
// ReadWikiIndexPaths (e.g. claudeMDPath points at a directory) propagates as a
// non-nil error rather than silently resolving to ScopeNoIndex.
func TestResolve_WikiRegistryReadError(t *testing.T) {
	dir := t.TempDir()
	// Pass a directory path as claudeMDPath — os.ReadFile on a dir returns a
	// non-IsNotExist error, so ReadWikiIndexPaths returns (nil, err).
	_, err := realm.Resolve(dir, dir)
	if err == nil {
		t.Fatal("expected a non-nil error when claudeMDPath is a directory, got nil")
	}
}

// TestResolve_NoWikisRegisteredNilError verifies the normal no-block case returns nil error.
func TestResolve_NoWikisRegisteredNilError(t *testing.T) {
	dir := t.TempDir()
	claudeMD := filepath.Join(dir, "CLAUDE.md")
	writeFile(t, claudeMD, "# no wikis block\n")

	_, err := realm.Resolve(dir, claudeMD)
	if err != nil {
		t.Fatalf("no-wikis-block case must return nil error, got: %v", err)
	}
}

// TestResolve_RealmRootDerivation verifies that realm root is correctly derived
// from a .../wiki/index.md path (parent of parent).
func TestResolve_RealmRootDerivation(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	// Manually verify derivation: wiki/index.md → Dir → wiki dir → Dir → realm root.
	wikiIndexPath := filepath.Join(root, "wiki", "index.md")
	derivedRoot := filepath.Dir(filepath.Dir(wikiIndexPath))
	if derivedRoot != root {
		t.Fatalf("derivation check: expected %q, got %q", root, derivedRoot)
	}

	members := []realm.MemberEntry{{Key: "myrepo", Path: "repos/myrepo"}}
	makeRealm(t, root, claudeMD, members)

	res, err := realm.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RealmRoot != root {
		t.Errorf("expected RealmRoot=%q, got %q", root, res.RealmRoot)
	}
}

// TestResolve_LocalIndexWinsOverRealm verifies local index short-circuits even
// when cwd is inside a registered realm.
func TestResolve_LocalIndexWinsOverRealm(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	members := []realm.MemberEntry{{Key: "foo", Path: "repos/foo"}}
	makeRealm(t, root, claudeMD, members)

	// Place a local index inside the member dir.
	fooDir := filepath.Join(root, "repos", "foo")
	dbPath := filepath.Join(fooDir, ".claude", ".atomic-index", "atomic.db")
	writeFile(t, dbPath, "")

	// cwd inside the member.
	res, err := realm.Resolve(fooDir, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeRepo {
		t.Errorf("expected ScopeRepo (local index wins), got %v", res.Scope)
	}
}

// TestResolve_RealmAllNoConfig verifies RealmAll when cwd == realm root but no code.toml.
func TestResolve_RealmAllNoConfig(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	// Register realm but don't write code.toml.
	writeFile(t, filepath.Join(root, "wiki", "index.md"), "# wiki\n")
	buildClaudeMD(t, claudeMD, []string{filepath.Join(root, "wiki", "index.md")})

	res, err := realm.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Scope != realm.ScopeRealmAll {
		t.Errorf("expected ScopeRealmAll, got %v", res.Scope)
	}
	// Config should be nil (absent file returns nil).
	if res.Config != nil {
		t.Error("expected nil Config when code.toml absent")
	}
}

// TestResolve_DBPaths verifies that DB path helpers produce the right path for realm members.
func TestResolve_DBPaths(t *testing.T) {
	root := t.TempDir()
	claudeMD := filepath.Join(root, "CLAUDE.md")

	members := []realm.MemberEntry{
		{Key: "alpha", Path: "repos/alpha"},
		{Key: "beta-v2", Path: "repos/beta"},
	}
	makeRealm(t, root, claudeMD, members)

	res, err := realm.Resolve(root, claudeMD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, m := range res.Config.Members {
		got := res.DBPath(m.Key)
		want := filepath.Join(root, ".atomic", m.Key+".db")
		if got != want {
			t.Errorf("DBPath(%q): expected %q, got %q", m.Key, want, got)
		}
	}
}
