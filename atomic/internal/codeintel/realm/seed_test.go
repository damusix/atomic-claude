package realm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

func buildWikiIndex(t *testing.T, indexPath string, members []struct{ path, status string }) {
	t.Helper()
	var block string
	block += "<wiki-scan generated=\"2026-01-01\" root=\"/realm\">\n"
	for _, m := range members {
		block += "  <repo path=\"" + m.path + "\" status=\"" + m.status + "\"/>\n"
	}
	block += "</wiki-scan>\n"
	writeFile(t, indexPath, "# wiki index\n\n"+block)
}

func TestSeedConfig_SeedsFromWikiScan(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{
		{"repos/alpha", "indexed"},
		{"repos/beta", "summarized"},
		{"repos/pending-repo", "pending"},
		{"trash/old-repo", "pending"},
	})

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Members) != 4 {
		t.Fatalf("expected 4 members, got %d: %+v", len(cfg.Members), cfg.Members)
	}

	byPath := make(map[string]realm.MemberEntry)
	for _, m := range cfg.Members {
		byPath[m.Path] = m
	}

	if m, ok := byPath["repos/alpha"]; !ok {
		t.Error("missing repos/alpha")
	} else {
		if m.Exclude {
			t.Error("repos/alpha should not be excluded")
		}
		if m.Key == "" {
			t.Error("key must not be empty")
		}
	}

	if m, ok := byPath["repos/beta"]; !ok {
		t.Error("missing repos/beta")
	} else if m.Exclude {
		t.Error("repos/beta should not be excluded (summarized)")
	}

	if m, ok := byPath["repos/pending-repo"]; !ok {
		t.Error("missing repos/pending-repo")
	} else if !m.Exclude {
		t.Error("repos/pending-repo should be excluded (pending)")
	}

	if m, ok := byPath["trash/old-repo"]; !ok {
		t.Error("missing trash/old-repo")
	} else if !m.Exclude {
		t.Error("trash/old-repo should be excluded (trash path)")
	}

	tomlPath := filepath.Join(dir, ".atomic", "code.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("code.toml not written: %v", err)
	}
}

func TestSeedConfig_SlugOnCollision(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{
		{"a/beta", "indexed"},
		{"b/beta", "indexed"},
		{"c/beta", "summarized"},
	})

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Members) != 3 {
		t.Fatalf("expected 3 members, got %v", cfg)
	}

	keys := make(map[string]bool)
	for _, m := range cfg.Members {
		if keys[m.Key] {
			t.Errorf("duplicate key %q", m.Key)
		}
		keys[m.Key] = true
	}
}

func TestSeedConfig_AppendDoesNotClobber(t *testing.T) {
	dir := t.TempDir()

	initialTOML := `[[member]]
key = "custom-key"
path = "repos/alpha"
exclude = false
`
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), initialTOML)

	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{
		{"repos/alpha", "indexed"}, // already in config
		{"repos/beta", "indexed"},  // new
	})

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Members) != 2 {
		t.Fatalf("expected 2 members, got %v", cfg)
	}

	if cfg.Members[0].Key != "custom-key" {
		t.Errorf("expected custom-key preserved, got %q", cfg.Members[0].Key)
	}
	if cfg.Members[0].Path != "repos/alpha" {
		t.Errorf("expected path=repos/alpha, got %q", cfg.Members[0].Path)
	}

	if cfg.Members[1].Path != "repos/beta" {
		t.Errorf("expected repos/beta appended, got %q", cfg.Members[1].Path)
	}
}

func TestSeedConfig_AbsentWikiIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "wiki", "index.md")

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("absent wiki index must not error, got: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when wiki index absent")
	}
}

func TestSeedConfig_NoNewMembersReturnsExisting(t *testing.T) {
	dir := t.TempDir()
	initialTOML := `[[member]]
key = "alpha"
path = "repos/alpha"
exclude = false
`
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), initialTOML)
	statBefore, _ := os.Stat(filepath.Join(dir, ".atomic", "code.toml"))

	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{
		{"repos/alpha", "indexed"},
	})

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Members) != 1 {
		t.Fatalf("expected 1 member, got %v", cfg)
	}

	statAfter, _ := os.Stat(filepath.Join(dir, ".atomic", "code.toml"))
	if statBefore.ModTime() != statAfter.ModTime() {
		t.Error("code.toml was rewritten even though no new members were added")
	}
}

// SeedConfig writes no dbs; this pins DBPath, which must never point inside a
// member directory.
func TestSeedConfig_DBPathAtRealm(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "wiki", "index.md")
	buildWikiIndex(t, indexPath, []struct{ path, status string }{
		{"repos/alpha", "indexed"},
	})

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || len(cfg.Members) != 1 {
		t.Fatalf("expected 1 member, got %v", cfg)
	}

	res := realm.Resolution{
		Scope:     realm.ScopeRealmAll,
		RealmRoot: dir,
		Members:   cfg.Members,
		Config:    cfg,
	}

	dbPath := res.DBPath(cfg.Members[0].Key)
	expectedPrefix := filepath.Join(dir, ".atomic") + string(filepath.Separator)
	if !hasPrefix(dbPath, expectedPrefix) {
		t.Errorf("DBPath %q does not start with realm .atomic dir %q", dbPath, expectedPrefix)
	}

	memberAbs := filepath.Join(dir, "repos", "alpha")
	if hasPrefix(dbPath, memberAbs) {
		t.Errorf("DBPath %q must not be inside member dir %q", dbPath, memberAbs)
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
