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

// Harness fingerprints never decide state: with all three set, an empty repo
// gets no index and a lone `.pi` index is not a candidate.
func TestResolve_CodeIndexHarnessFingerprintDoesNotDecide(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ATOMIC_HARNESS", "pi")
	t.Setenv("PI_CODING_AGENT", "true")
	t.Setenv("CLAUDECODE", "1")

	// No candidate directory exists, so the fingerprint has nothing to select.
	empty := t.TempDir()
	mkGitMarker(t, empty)
	report, err := where.Resolve(empty, missingClaudeMD(t, empty))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.CodeIndex.Scope != realm.ScopeNoIndex {
		t.Errorf("fingerprint invented an index; scope = %v, want NoIndex", report.CodeIndex.Scope)
	}

	pi := t.TempDir()
	mkGitMarker(t, pi)
	index := filepath.Join(pi, ".pi", ".atomic-index")
	if err := os.MkdirAll(index, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(index, "atomic.db"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = where.Resolve(pi, missingClaudeMD(t, pi))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if report.CodeIndex.Scope != realm.ScopeNoIndex {
		t.Errorf("a .pi index was resolved; scope = %v, want NoIndex", report.CodeIndex.Scope)
	}
}

// Creating the authority by registering one realm must keep every realm that only
// lived in the legacy <wikis> block resolvable.
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
	if err := wiki.RegisterAndProject(home, filepath.Join(newRoot, "wiki", "index.md"), []string{claudeMD}); err != nil {
		t.Fatalf("RegisterAndProject: %v", err)
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
