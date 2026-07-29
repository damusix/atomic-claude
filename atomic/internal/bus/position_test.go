package bus

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestResolvePosition_OutsideRepo_UsableDefault proves the "outside any
// repo" success criterion: where.Resolve's own repo-root fallback (no
// marker, no .git) is cwd itself — so repo is never left empty just because
// cwd sits outside version control, and a name built from it alone is still
// usable.
func TestResolvePosition_OutsideRepo_UsableDefault(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	want := filepath.Base(cwd)

	pos, err := resolvePosition(home, cwd)
	if err != nil {
		t.Fatalf("resolvePosition: %v", err)
	}
	if pos.repo != want {
		t.Errorf("repo = %q, want %q", pos.repo, want)
	}
	if pos.realm != "" {
		t.Errorf("realm = %q, want empty (not inside any realm)", pos.realm)
	}
}

// TestResolvePosition_RepoMarker_UsesMarkerRootBasename proves a
// scope="repo" marker (.claude/atomic.toml) is honored by where.Resolve —
// cwd under (not at) the marker root still resolves to the marker root's
// own basename, not cwd's.
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

// TestPosition_Name_DelegatesToStackedName proves position.name is a thin
// wrapper over stackedName(p.realm, p.repo, as) — the collapse matrix itself
// is covered exhaustively by TestStackedName_CollapsesDuplicateAdjacentSegments;
// this only pins the wiring.
func TestPosition_Name_DelegatesToStackedName(t *testing.T) {
	p := position{realm: "myrealm", repo: "atomic-claude"}
	got := p.name("backend")
	want := stackedName("myrealm", "atomic-claude", "backend")
	if got != want {
		t.Errorf("p.name(%q) = %q, want %q (stackedName(%q, %q, %q))", "backend", got, want, p.realm, p.repo, "backend")
	}
}

// TestStackedName_CollapsesDuplicateAdjacentSegments covers the full
// collapse matrix directly against stackedName: every segment present,
// each segment missing in turn, a segment equal to the one before it (in
// every position that can produce that), and every-segment-empty. Half of
// this matrix (name-equals-repo, realm-equals-repo) is the common case once
// --as often echoes the repo it's role-suffixing, and is exactly the case a
// thinner test (only empty-segment coverage) would miss.
func TestStackedName_CollapsesDuplicateAdjacentSegments(t *testing.T) {
	cases := []struct {
		name            string
		realm, repo, as string
		want            string
	}{
		{"all three segments distinct", "myrealm", "atomic-claude", "backend", "myrealm-atomic-claude-backend"},
		{"missing realm", "", "atomic-claude", "backend", "atomic-claude-backend"},
		{"missing --as", "myrealm", "atomic-claude", "", "myrealm-atomic-claude"},
		{"missing realm and --as", "", "atomic-claude", "", "atomic-claude"},
		{"as alone, no realm, no repo", "", "", "role", "role"},
		{"all three empty", "", "", "", ""},
		{"name equals repo collapses", "", "repo-alpha", "repo-alpha", "repo-alpha"},
		{"realm equals repo collapses", "repo-alpha", "repo-alpha", "agent", "repo-alpha-agent"},
		{"all three equal collapses to one segment", "x", "x", "x", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stackedName(tc.realm, tc.repo, tc.as); got != tc.want {
				t.Errorf("stackedName(%q, %q, %q) = %q, want %q", tc.realm, tc.repo, tc.as, got, tc.want)
			}
		})
	}
}
