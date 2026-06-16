package serve

// code_members_test.go — unit tests for realm code-member discovery. These run
// in-package (package serve) because discoverCodeMembers / memberForPath are
// unexported resolution helpers; their behavior is the contract that makes the
// federated code modal and code search find a member's own
// .claude/.atomic-index/atomic.db when the realm has no <code-index> federation.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

func touchIndex(t *testing.T, repoRoot string) string {
	t.Helper()
	db := filepath.Join(repoRoot, ".claude", ".atomic-index", "atomic.db")
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

// A wiki realm with NO <code-index> federation, where one member was indexed the
// natural way (cd member; atomic code index). Discovery must surface that member.
func TestDiscoverCodeMembers_SelfIndexedMember(t *testing.T) {
	realmRoot := t.TempDir()
	touchIndex(t, filepath.Join(realmRoot, "monorepo"))
	// A second member with no index must NOT appear (would be search noise).
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
	if m.DBPath != filepath.Join(realmRoot, "monorepo", ".claude", ".atomic-index", "atomic.db") {
		t.Errorf("DBPath = %q, want the member self-index", m.DBPath)
	}
	if m.Path != filepath.Join(realmRoot, "monorepo") {
		t.Errorf("Path = %q, want the member repo root", m.Path)
	}
}

// Repo scope (a bare indexed repo, not a realm): one member, empty prefix, files
// served at the root.
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

// memberForPath maps a realm-relative file path to its owning member and returns
// the member-relative remainder used to query that member's index.
func TestMemberForPath_LongestPrefixAndRemainder(t *testing.T) {
	members := []codeMember{
		{Key: "monorepo", Prefix: "monorepo", Path: "/r/monorepo", DBPath: "/r/monorepo/db"},
		{Key: "monorepo/packages/ui", Prefix: "monorepo/packages/ui", Path: "/r/monorepo/packages/ui", DBPath: "/r/monorepo/packages/ui/db"},
	}
	// A nested member must win over its ancestor (longest-prefix).
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

	// A path under the ancestor only.
	m, rem, ok = memberForPath(members, "monorepo/Apps/workers/src/x.ts")
	if !ok || m.Prefix != "monorepo" || rem != "Apps/workers/src/x.ts" {
		t.Errorf("ancestor match wrong: ok=%v prefix=%q rem=%q", ok, m.Prefix, rem)
	}

	// No member owns this path.
	if _, _, ok := memberForPath(members, "other/thing.go"); ok {
		t.Error("expected no match for an unowned path")
	}
}

// A single repo-scope member (empty prefix) owns every path, remainder unchanged.
func TestMemberForPath_RepoScopeMatchesAll(t *testing.T) {
	members := []codeMember{{Key: "", Prefix: "", Path: "/r", DBPath: "/r/db"}}
	m, rem, ok := memberForPath(members, "internal/foo/bar.go")
	if !ok || m.Prefix != "" || rem != "internal/foo/bar.go" {
		t.Errorf("repo-scope mapping wrong: ok=%v prefix=%q rem=%q", ok, m.Prefix, rem)
	}
}
