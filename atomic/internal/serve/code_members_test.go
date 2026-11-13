package serve

// discoverCodeMembers and memberForPath are what let the code modal and code
// search reach a member's own index when the realm has no <code-index>
// federation at all.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// A member whose state root refuses falls back to the federation db rather
// than exiting the server.
func TestMemberDB_RefusingMemberFallsBackToFederationDB(t *testing.T) {
	realmRoot := t.TempDir()
	member := filepath.Join(realmRoot, "api")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	refuseStateRoot(t, member)

	fed := filepath.Join(realmRoot, "fed.db")
	if got := memberDB(realmRoot, "api", fed); got != fed {
		t.Errorf("memberDB = %q, want the federation db %q", got, fed)
	}
}

// Members resolve on every request, so a refusing member must not repeat its
// warning on each one.
func TestMemberDB_RefusingMemberWarnsOnce(t *testing.T) {
	realmRoot := t.TempDir()
	member := filepath.Join(realmRoot, "api")
	if err := os.MkdirAll(member, 0o755); err != nil {
		t.Fatal(err)
	}
	refuseStateRoot(t, member)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prev })
	memberDB(realmRoot, "api", "")
	memberDB(realmRoot, "api", "")
	os.Stderr = prev
	w.Close()
	out, _ := io.ReadAll(r)

	if n := strings.Count(string(out), "\n"); n != 1 {
		t.Errorf("printed %d warning lines, want 1:\n%s", n, out)
	}
}

func touchIndex(t *testing.T, repoRoot string) string {
	t.Helper()
	db := config.IndexDBPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatalf("write db: %v", err)
	}
	return db
}

func writeWikiScan(t *testing.T, realmRoot string, memberPaths []string) string {
	t.Helper()
	idx := filepath.Join(realmRoot, "wiki", "index.md")
	if err := os.MkdirAll(filepath.Dir(idx), 0o755); err != nil {
		t.Fatalf("mkdir wiki: %v", err)
	}
	var b []byte
	b = append(b, []byte("# wiki\n\n<wiki-scan generated=\"2026-01-01\" root=\""+realmRoot+"\">\n")...)
	for _, p := range memberPaths {
		b = append(b, []byte("<repo path=\""+p+"\" status=\"summarized\" summary=\"wiki/repos/"+filepath.Base(p)+".md\">\n")...)
	}
	b = append(b, []byte("</wiki-scan>\n")...)
	if err := os.WriteFile(idx, b, 0o644); err != nil {
		t.Fatalf("write wiki index: %v", err)
	}
	return idx
}

func TestDiscoverCodeMembers_SelfIndexedMember(t *testing.T) {
	realmRoot := t.TempDir()
	touchIndex(t, filepath.Join(realmRoot, "monorepo"))
	// Unindexed, so it must stay out of results — it would be search noise.
	_ = os.MkdirAll(filepath.Join(realmRoot, "brea-mls"), 0o755)
	wikiIdx := writeWikiScan(t, realmRoot, []string{"monorepo", "brea-mls"})

	res := realm.Resolution{Scope: realm.ScopeRealmAll, RealmRoot: realmRoot}
	members := discoverCodeMembers(res, realmRoot, wikiIdx)

	if len(members) != 1 {
		t.Fatalf("want 1 indexed member, got %d: %+v", len(members), members)
	}
	m := members[0]
	if m.Prefix != "monorepo" {
		t.Errorf("Prefix = %q, want monorepo", m.Prefix)
	}
	if m.DBPath != config.IndexDBPath(filepath.Join(realmRoot, "monorepo")) {
		t.Errorf("DBPath = %q, want the member self-index", m.DBPath)
	}
	if m.Path != filepath.Join(realmRoot, "monorepo") {
		t.Errorf("Path = %q, want the member repo root", m.Path)
	}
}

// A bare indexed repo is one member with an empty prefix, served at the root.
func TestDiscoverCodeMembers_RepoScope(t *testing.T) {
	root := t.TempDir()
	touchIndex(t, root)
	res := realm.Resolution{Scope: realm.ScopeRepo}
	members := discoverCodeMembers(res, root, "")
	if len(members) != 1 {
		t.Fatalf("want 1 member, got %d", len(members))
	}
	if members[0].Prefix != "" {
		t.Errorf("repo scope Prefix = %q, want empty", members[0].Prefix)
	}
}

// The state segment is configurable, so neither path may hardcode it.
func TestDiscoverCodeMembers_RepoScope_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetStateDirForTest(".pi")
	defer restore()

	root := t.TempDir()
	res := realm.Resolution{Scope: realm.ScopeRepo}
	members := discoverCodeMembers(res, root, "")
	if len(members) != 1 {
		t.Fatalf("want 1 member, got %d", len(members))
	}
	want := config.IndexDBPath(root)
	if members[0].DBPath != want {
		t.Errorf("DBPath = %q, want %q", members[0].DBPath, want)
	}

	mr := memberResolver{realmRoot: root}
	if got := mr.localDBPath(); got != want {
		t.Errorf("localDBPath() = %q, want %q", got, want)
	}
}

// The remainder is what gets handed to the owning member's index.
func TestMemberForPath_LongestPrefixAndRemainder(t *testing.T) {
	members := []codeMember{
		{Key: "monorepo", Prefix: "monorepo", Path: "/r/monorepo", DBPath: "/r/monorepo/db"},
		{Key: "monorepo/packages/ui", Prefix: "monorepo/packages/ui", Path: "/r/monorepo/packages/ui", DBPath: "/r/monorepo/packages/ui/db"},
	}
	// Longest prefix wins, so the nested member beats its ancestor.
	m, rem, ok := memberForPath(members, "monorepo/packages/ui/src/Button.tsx")
	if !ok {
		t.Fatal("expected a match")
	}
	if m.Prefix != "monorepo/packages/ui" {
		t.Errorf("matched %q, want the nested member", m.Prefix)
	}
	if rem != "src/Button.tsx" {
		t.Errorf("remainder = %q, want src/Button.tsx", rem)
	}

	m, rem, ok = memberForPath(members, "monorepo/Apps/workers/src/x.ts")
	if !ok || m.Prefix != "monorepo" || rem != "Apps/workers/src/x.ts" {
		t.Errorf("ancestor match wrong: ok=%v prefix=%q rem=%q", ok, m.Prefix, rem)
	}

	if _, _, ok := memberForPath(members, "other/thing.go"); ok {
		t.Error("expected no match for an unowned path")
	}
}

// An empty prefix owns every path, and the remainder passes through untouched.
func TestMemberForPath_RepoScopeMatchesAll(t *testing.T) {
	members := []codeMember{{Key: "", Prefix: "", Path: "/r", DBPath: "/r/db"}}
	m, rem, ok := memberForPath(members, "internal/foo/bar.go")
	if !ok || m.Prefix != "" || rem != "internal/foo/bar.go" {
		t.Errorf("repo-scope mapping wrong: ok=%v prefix=%q rem=%q", ok, m.Prefix, rem)
	}
}
