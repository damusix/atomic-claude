package bus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestResolvePosition_OutsideRepo_UsableDefault proves the "outside any
// repo" success criterion: repoctx.ResolveFrom's ScopeSourceCwd fallback
// still yields a usable default name, and where.Resolve's own repo-root
// fallback (no marker, no .git) is cwd itself — so repo is never left
// empty just because cwd sits outside version control.
func TestResolvePosition_OutsideRepo_UsableDefault(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	want := filepath.Base(cwd)

	pos, err := resolvePosition(home, cwd)
	if err != nil {
		t.Fatalf("resolvePosition: %v", err)
	}
	if pos.defaultName != want {
		t.Errorf("defaultName = %q, want %q", pos.defaultName, want)
	}
	if pos.repo != want {
		t.Errorf("repo = %q, want %q", pos.repo, want)
	}
	if pos.realm != "" {
		t.Errorf("realm = %q, want empty (not inside any realm)", pos.realm)
	}
}

// TestResolvePosition_RepoMarker_UsesMarkerRootBasename proves a
// scope="repo" marker (.claude/atomic.toml) is honored by both resolvers a
// join under it consults — repoctx.ResolveFrom for the --as default and
// where.Resolve for Member.Repo — so cwd under (not at) the marker root
// still resolves to the marker root's own basename, not cwd's.
func TestResolvePosition_RepoMarker_UsesMarkerRootBasename(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	if _, err := config.EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	want := filepath.Base(root)

	pos, err := resolvePosition(home, sub)
	if err != nil {
		t.Fatalf("resolvePosition: %v", err)
	}
	if pos.defaultName != want {
		t.Errorf("defaultName = %q, want %q (marker root basename)", pos.defaultName, want)
	}
	if pos.repo != want {
		t.Errorf("repo = %q, want %q (marker root basename)", pos.repo, want)
	}
}

// TestResolvePosition_RealmMarker_CwdAtRealmRoot proves a scope="realm"
// marker resolves realm to the realm root's own basename when cwd is that
// root.
func TestResolvePosition_RealmMarker_CwdAtRealmRoot(t *testing.T) {
	home := t.TempDir()
	realmRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	want := filepath.Base(realmRoot)

	pos, err := resolvePosition(home, realmRoot)
	if err != nil {
		t.Fatalf("resolvePosition: %v", err)
	}
	if pos.realm != want {
		t.Errorf("realm = %q, want %q", pos.realm, want)
	}
}

// TestResolvePosition_RealmMarker_OrphanedSubdirStillGetsRealm proves realm
// is populated for any non-None RealmScope.Position, not only RealmRoot —
// a subdirectory of a registered realm that is not itself a registered
// member (no wiki/index.md <wiki-scan> entry) is "orphaned", not "none",
// and the spec's own wording ("realm root basename when the session is
// inside one") covers that case too: cwd is genuinely inside the realm's
// directory tree.
func TestResolvePosition_RealmMarker_OrphanedSubdirStillGetsRealm(t *testing.T) {
	home := t.TempDir()
	realmRoot := t.TempDir()
	if _, err := config.EnsureScopeMarker(realmRoot, "realm"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	sub := filepath.Join(realmRoot, "unregistered")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	want := filepath.Base(realmRoot)

	pos, err := resolvePosition(home, sub)
	if err != nil {
		t.Fatalf("resolvePosition: %v", err)
	}
	if pos.realm != want {
		t.Errorf("realm = %q, want %q (orphaned is still inside the realm)", pos.realm, want)
	}
}
