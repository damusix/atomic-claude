package realm_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// buildWikiIndex writes a wiki/index.md with a <wiki-scan> block listing the
// provided members (path + status).
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

// ─── SeedConfig tests ────────────────────────────────────────────────────────

// TestSeedConfig_SeedsFromWikiScan verifies that seeding from a fresh wiki index
// produces the expected TOML entries with correct keys and exclude flags.
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

	// alpha: indexed → exclude=false
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

	// beta: summarized → exclude=false
	if m, ok := byPath["repos/beta"]; !ok {
		t.Error("missing repos/beta")
	} else if m.Exclude {
		t.Error("repos/beta should not be excluded (summarized)")
	}

	// pending-repo: pending → exclude=true
	if m, ok := byPath["repos/pending-repo"]; !ok {
		t.Error("missing repos/pending-repo")
	} else if !m.Exclude {
		t.Error("repos/pending-repo should be excluded (pending)")
	}

	// trash/old-repo: trash path → exclude=true
	if m, ok := byPath["trash/old-repo"]; !ok {
		t.Error("missing trash/old-repo")
	} else if !m.Exclude {
		t.Error("trash/old-repo should be excluded (trash path)")
	}

	// Verify code.toml was written.
	tomlPath := filepath.Join(dir, ".atomic", "code.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("code.toml not written: %v", err)
	}
}

// TestSeedConfig_SlugOnCollision verifies that two members with the same
// basename get different keys (e.g. "beta" and "beta-2").
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

// TestSeedConfig_AppendDoesNotClobber verifies that re-seeding appends only new
// members and does not overwrite existing entries or manual edits.
func TestSeedConfig_AppendDoesNotClobber(t *testing.T) {
	dir := t.TempDir()

	// Write an initial code.toml with a manually edited entry.
	initialTOML := `[[member]]
key = "custom-key"
path = "repos/alpha"
exclude = false
`
	writeFile(t, filepath.Join(dir, ".atomic", "code.toml"), initialTOML)

	// Wiki now also lists beta.
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

	// The alpha entry must preserve the manually edited key.
	if cfg.Members[0].Key != "custom-key" {
		t.Errorf("expected custom-key preserved, got %q", cfg.Members[0].Key)
	}
	if cfg.Members[0].Path != "repos/alpha" {
		t.Errorf("expected path=repos/alpha, got %q", cfg.Members[0].Path)
	}

	// Beta was appended.
	if cfg.Members[1].Path != "repos/beta" {
		t.Errorf("expected repos/beta appended, got %q", cfg.Members[1].Path)
	}
}

// TestSeedConfig_AbsentWikiIndex returns nil config without error when the
// wiki/index.md does not exist.
func TestSeedConfig_AbsentWikiIndex(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "wiki", "index.md")
	// indexPath not created.

	cfg, err := realm.SeedConfig(dir, indexPath)
	if err != nil {
		t.Fatalf("absent wiki index must not error, got: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when wiki index absent")
	}
}

// TestSeedConfig_NoNewMembersReturnsExisting verifies that when all wiki members
// are already in code.toml, SeedConfig returns the existing config unchanged
// (no new write).
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
	// File should not have been rewritten (mtime unchanged).
	if statBefore.ModTime() != statAfter.ModTime() {
		t.Error("code.toml was rewritten even though no new members were added")
	}
}

// TestSeedConfig_DBNotWrittenIntoMemberDir verifies SC 3: the realm db files
// are placed at <realm>/.atomic/<key>.db, never inside member directories.
// SeedConfig itself doesn't write dbs; this verifies DBPath helper.
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

	// Construct a Resolution to test DBPath.
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

	// Confirm the path is NOT inside any member directory.
	memberAbs := filepath.Join(dir, "repos", "alpha")
	if hasPrefix(dbPath, memberAbs) {
		t.Errorf("DBPath %q must not be inside member dir %q", dbPath, memberAbs)
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
