package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func realmMarker(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".atomic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic.toml"), []byte("scope = \"realm\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindRealm_MarkerAbovePath(t *testing.T) {
	realm := t.TempDir()
	realmMarker(t, realm)
	sub := filepath.Join(realm, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	loc, ok, err := FindRealm(sub, claudeMDPathFor(t, t.TempDir()))
	if err != nil || !ok {
		t.Fatalf("FindRealm = (%+v, %v, %v), want a realm", loc, ok, err)
	}
	if loc.Root != realm || loc.Source != config.ScopeSourceMarker ||
		loc.IndexPath != filepath.Join(realm, "wiki", "index.md") {
		t.Errorf("location = %+v", loc)
	}
}

func TestFindRealm_RegistryFallbackWithoutMarker(t *testing.T) {
	claudeMD := claudeMDPathFor(t, t.TempDir())
	realm := t.TempDir()
	index := filepath.Join(realm, "wiki", "index.md")
	if err := RegisterAndProject(filepath.Dir(filepath.Dir(claudeMD)), index, []string{claudeMD}); err != nil {
		t.Fatal(err)
	}

	loc, ok, err := FindRealm(filepath.Join(realm, "app"), claudeMD)
	if err != nil || !ok {
		t.Fatalf("FindRealm = (%+v, %v, %v), want a realm", loc, ok, err)
	}
	if loc.Root != realm || loc.Source != config.ScopeSourceRegistry || loc.IndexPath != index {
		t.Errorf("location = %+v", loc)
	}
}

func TestFindRealm_NoneIsNotAnError(t *testing.T) {
	_, ok, err := FindRealm(t.TempDir(), claudeMDPathFor(t, t.TempDir()))
	if ok || err != nil {
		t.Errorf("FindRealm = (%v, %v), want no realm and no error", ok, err)
	}
}

func TestFindRealm_SiblingWithSharedPrefixIsNotInside(t *testing.T) {
	claudeMD := claudeMDPathFor(t, t.TempDir())
	parent := t.TempDir()
	realm := filepath.Join(parent, "realm")
	if err := RegisterAndProject(filepath.Dir(filepath.Dir(claudeMD)), filepath.Join(realm, "wiki", "index.md"), []string{claudeMD}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := FindRealm(filepath.Join(parent, "realm-other"), claudeMD); ok {
		t.Error("realm-other matched the realm registered at realm")
	}
}
