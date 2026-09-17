package where_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/where"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// A realm registered in the authoritative ~/.atomic/wikis.md resolves even when
// the harness-native CLAUDE.md projection does not exist.
func TestResolve_RealmFromRegistryAuthority(t *testing.T) {
	home := t.TempDir()
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")

	realmRoot := t.TempDir()
	indexPath := mkRealmWikiIndex(t, realmRoot, "member-a")
	if _, _, err := wiki.NewWikiRegistry(home).Add(indexPath); err != nil {
		t.Fatalf("register authority: %v", err)
	}

	report, err := where.Resolve(realmRoot, claudeMD)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.RealmScope.Position != where.RealmRoot {
		t.Errorf("position = %v, want root", report.RealmScope.Position)
	}
	if report.RealmScope.Source != config.ScopeSourceRegistry {
		t.Errorf("source = %q, want %q", report.RealmScope.Source, config.ScopeSourceRegistry)
	}
}

// A changed projection is not an input: the authority still decides.
func TestResolve_ChangedProjectionDoesNotOverrideAuthority(t *testing.T) {
	home := t.TempDir()
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")

	registered := t.TempDir()
	registeredIndex := mkRealmWikiIndex(t, registered, "member-a")
	rogue := t.TempDir()
	rogueIndex := mkRealmWikiIndex(t, rogue, "member-b")

	if _, _, err := wiki.NewWikiRegistry(home).Add(registeredIndex); err != nil {
		t.Fatalf("register authority: %v", err)
	}
	if err := os.WriteFile(claudeMD, []byte("<wikis>\n- "+rogueIndex+"\n</wikis>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(rogue, claudeMD)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.RealmScope.Position != where.RealmNone {
		t.Errorf("changed projection overrode the authority: position=%v", report.RealmScope.Position)
	}

	report, err = where.Resolve(registered, claudeMD)
	if err != nil {
		t.Fatalf("Resolve registered: %v", err)
	}
	if report.RealmScope.Position != where.RealmRoot {
		t.Errorf("authority realm = %v, want root", report.RealmScope.Position)
	}
}

// Repository-state resolution is harness-neutral: harness fingerprints never
// select the code-index root. Only a `.pi` index exists, so the report must not
// claim a repo-scope index.
func TestResolve_CodeIndexIgnoresHarnessFingerprints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATOMIC_HARNESS", "pi")
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")

	repo := t.TempDir()
	mkGitMarker(t, repo)
	piIndex := filepath.Join(repo, ".pi", ".atomic-index")
	if err := os.MkdirAll(piIndex, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piIndex, "atomic.db"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := where.Resolve(repo, missingClaudeMD(t, repo))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.CodeIndex.Scope == realm.ScopeRepo {
		t.Errorf("harness fingerprint selected the .pi index; scope=%v", report.CodeIndex.Scope)
	}

	// The default `.claude` layout is what the neutral ladder selects.
	claudeIndex := filepath.Join(repo, ".claude", ".atomic-index")
	if err := os.MkdirAll(claudeIndex, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeIndex, "atomic.db"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = where.Resolve(repo, missingClaudeMD(t, repo))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.CodeIndex.Scope != realm.ScopeRepo {
		t.Errorf("default .claude index not selected; scope=%v", report.CodeIndex.Scope)
	}
}

// The legacy <wikis> block is the pre-authority registry. Registering one more
// realm through the authority must keep every realm that only ever lived in the
// block resolvable, not just the newly registered one.
func TestResolve_LegacyBlockRealmsSurviveAuthorityCreation(t *testing.T) {
	home := t.TempDir()
	claudeHome := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeHome, 0o755); err != nil {
		t.Fatal(err)
	}
	claudeMD := filepath.Join(claudeHome, "CLAUDE.md")

	legacyRoots := []string{t.TempDir(), t.TempDir()}
	var block strings.Builder
	block.WriteString("<wikis>\n")
	for _, root := range legacyRoots {
		block.WriteString("- " + filepath.Join(root, "wiki", "index.md") + "\n")
	}
	block.WriteString("</wikis>\n")
	if err := os.WriteFile(claudeMD, []byte(block.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	newRoot := t.TempDir()
	if err := wiki.RegisterWiki(claudeMD, filepath.Join(newRoot, "wiki", "index.md")); err != nil {
		t.Fatalf("RegisterWiki: %v", err)
	}

	paths, err := wiki.NewWikiRegistry(home).Load()
	if err != nil {
		t.Fatalf("authority Load: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("authority entries = %v, want all three realms", paths)
	}

	for _, root := range append(legacyRoots, newRoot) {
		report, err := where.Resolve(root, claudeMD)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", root, err)
		}
		if report.RealmScope.Position != where.RealmRoot {
			t.Errorf("realm %s position = %v, want root", root, report.RealmScope.Position)
		}
	}
}
