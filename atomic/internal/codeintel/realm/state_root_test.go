package realm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

const alphaTOML = "[[member]]\nkey = \"alpha\"\npath = \"repos/alpha\"\n"

func selectRealmRoot(t *testing.T, realmRoot, stateDir string) {
	t.Helper()
	if _, err := config.PersistStateSelection(realmRoot, stateDir); err != nil {
		t.Fatalf("PersistStateSelection: %v", err)
	}
}

func forRealm(t *testing.T, dir string) realm.Resolution {
	t.Helper()
	res, err := realm.ForRealm(dir)
	if err != nil {
		t.Fatalf("ForRealm: %v", err)
	}
	return res
}

// A realm with no record or populated candidate keeps federation where it
// always lived, so existing realms need no move.
func TestFederation_DefaultsToDotAtomic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), alphaTOML)

	cfgPath, err := realm.ConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".atomic", "code.toml"); cfgPath != want {
		t.Errorf("ConfigPath = %q, want %q", cfgPath, want)
	}
	if cfg, err := realm.LoadConfig(dir); err != nil || cfg == nil || len(cfg.Members) != 1 {
		t.Errorf("LoadConfig = %v, %v; want the one member", cfg, err)
	}
}

// A custom realm root carries code.toml and the member databases.
func TestFederation_CustomRealmRootCarriesConfigAndDatabases(t *testing.T) {
	dir := t.TempDir()
	selectRealmRoot(t, dir, "custom/state")
	writeFile(t, filepath.Join(dir, "custom", "state", "code.toml"), alphaTOML)
	writeFile(t, filepath.Join(dir, "custom", "state", "alpha.db"), "db")

	cfg, err := realm.LoadConfig(dir)
	if err != nil || cfg == nil || len(cfg.Members) != 1 {
		t.Fatalf("LoadConfig = %v, %v; want the custom root's member", cfg, err)
	}
	res := forRealm(t, dir)
	if got, want := res.DBPath("alpha"), filepath.Join(dir, "custom", "state", "alpha.db"); got != want {
		t.Errorf("DBPath = %q, want %q", got, want)
	}
	if got, err := realm.WriteDBPath(dir, "alpha"); err != nil || got != filepath.Join(dir, "custom", "state", "alpha.db") {
		t.Errorf("WriteDBPath = %q, %v", got, err)
	}
}

// A reader accepts the legacy <realm>/.atomic/ files until a re-index writes
// the resolved root, and the resolved root wins once it has the file.
func TestFederation_ReaderAcceptsLegacyLocation(t *testing.T) {
	dir := t.TempDir()
	selectRealmRoot(t, dir, "custom/state")
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), alphaTOML)
	writeFile(t, filepath.Join(dir, ".atomic", "alpha.db"), "legacy")

	if cfg, err := realm.LoadConfig(dir); err != nil || cfg == nil || len(cfg.Members) != 1 {
		t.Fatalf("legacy code.toml not read: %v, %v", cfg, err)
	}
	res := forRealm(t, dir)
	if got, want := res.DBPath("alpha"), filepath.Join(dir, ".atomic", "alpha.db"); got != want {
		t.Errorf("legacy DBPath = %q, want %q", got, want)
	}
	if got := res.DBPath("unindexed"); got != filepath.Join(dir, "custom", "state", "unindexed.db") {
		t.Errorf("an absent database must resolve to the custom root, got %q", got)
	}

	writeFile(t, filepath.Join(dir, "custom", "state", "alpha.db"), "fresh")
	if got, want := res.DBPath("alpha"), filepath.Join(dir, "custom", "state", "alpha.db"); got != want {
		t.Errorf("resolved root must win over legacy: DBPath = %q, want %q", got, want)
	}
}

// A refused state root is an error for every federation entry point, never
// papered over with a default path.
func TestFederation_RefusedStateRootSurfaces(t *testing.T) {
	t.Setenv(config.StateDirEnvVar, "/")
	dir := t.TempDir()

	if _, err := realm.ForRealm(dir); err == nil {
		t.Error("ForRealm: want the refusal, got nil")
	}
	if _, err := realm.LoadConfig(dir); err == nil {
		t.Error("LoadConfig: want the refusal, got nil")
	}
	if _, err := realm.ConfigPath(dir); err == nil {
		t.Error("ConfigPath: want the refusal, got nil")
	}
}

// A realm whose marker sits in the legacy .claude keeps federation writes in
// <realm>/.atomic/, where existing realms already hold them.
func TestFederation_LegacyClaudeRealmWritesDotAtomic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude", "atomic.toml"), "scope = \"realm\"\n")

	cfgPath, err := realm.ConfigPath(dir)
	if err != nil || cfgPath != filepath.Join(dir, ".atomic", "code.toml") {
		t.Errorf("ConfigPath = %q, %v; want under .atomic", cfgPath, err)
	}
	dbPath, err := realm.WriteDBPath(dir, "alpha")
	if err != nil || dbPath != filepath.Join(dir, ".atomic", "alpha.db") {
		t.Errorf("WriteDBPath = %q, %v; want under .atomic", dbPath, err)
	}
	if err := realm.EnsureDBIgnore(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atomic", ".gitignore")); err != nil {
		t.Errorf("/*.db rule not written under .atomic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", ".gitignore")); err == nil {
		t.Error("nothing federation-shaped belongs under .claude")
	}
}

// A state root outside the realm gets no ignore rule.
func TestFederation_OutsideRealmRootGetsNoIgnoreRule(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	selectRealmRoot(t, dir, outside)

	if err := realm.EnsureDBIgnore(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".gitignore")); err == nil {
		t.Error("a state root outside the realm must get no ignore rule")
	}
}

// Writers never touch the legacy location: seeding lands in the resolved root.
func TestFederation_SeedWritesResolvedRoot(t *testing.T) {
	dir := t.TempDir()
	selectRealmRoot(t, dir, "custom/state")
	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{{"repos/alpha", "indexed"}})

	if _, err := realm.SeedConfig(dir, indexPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "custom", "state", "code.toml")); err != nil {
		t.Errorf("seeded code.toml missing from the custom root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".atomic", "code.toml")); err == nil {
		t.Error("seeding must not write the legacy location")
	}
}

// Member databases are rebuildable output, so the realm root's own .gitignore
// ignores them; a second call adds nothing.
func TestFederation_IgnoresDatabases(t *testing.T) {
	dir := t.TempDir()
	selectRealmRoot(t, dir, "custom/state")

	for range 2 {
		if err := realm.EnsureDBIgnore(dir); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "custom", "state", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "/*.db"); got != 1 {
		t.Errorf(".gitignore carries /*.db %d times:\n%s", got, data)
	}
}
