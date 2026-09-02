package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// TestPlansHandler_ServesRowsFromAggregator drives the real handler end to
// end, rather than calling the aggregator directly — this is the surface
// api_plans_page_test.go's fixtures depend on to hand back a worktree id.
func TestPlansHandler_ServesRowsFromAggregator(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	writeDoc(t, main, "spec", "my-feature", "# my-feature\n\n## Goal\n\nDo the thing.\n", time.Now().Add(-time.Minute))

	h := plansHandler(plansOptions{Root: main, Registry: newPlansRegistry()})

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var rows []planRow
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	row := findRow(t, rows, "my-feature")
	if row.Title != "my-feature" {
		t.Errorf("Title = %q, want my-feature", row.Title)
	}
}

// A member param is only meaningful in realm scope; outside one, plansMembers
// returns no candidates, so any requested key is unknown.
func TestPlansHandler_UnknownMemberRejected(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	h := plansHandler(plansOptions{Root: main, ScopeRoot: main, Registry: newPlansRegistry()})

	req := httptest.NewRequest(http.MethodGet, "/api/plans?member=nope", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// buildRealmFixture wires a minimal realm: a CLAUDE.md <wikis> block, a
// wiki/index.md, and a .atomic/code.toml declaring one member per key. Each
// member path is its own git repo, since the aggregator enumerates worktrees
// via `git worktree list`. Each member also gets its own docs/spec/<key>-slug.md
// (a distinct slug per member, so a scoping test can tell rows apart) and its
// own `git worktree add` (a distinct worktree id per member, so a scoping
// test can tell checkouts apart) — CP16's realm-scoping proof needs both.
func buildRealmFixture(t *testing.T, memberKeys ...string) (realmRoot, claudeMD string) {
	t.Helper()
	realmRoot = t.TempDir()
	claudeMD = filepath.Join(realmRoot, "CLAUDE.md")

	writeFile(t, filepath.Join(realmRoot, "wiki", "index.md"), "# wiki\n")
	writeFile(t, claudeMD, "# CLAUDE.md\n\n<wikis>\n- "+filepath.Join(realmRoot, "wiki", "index.md")+"\n</wikis>\n")

	toml := ""
	for _, key := range memberKeys {
		toml += "[[member]]\nkey = \"" + key + "\"\npath = \"" + key + "\"\nexclude = false\n\n"
		memberDir := filepath.Join(realmRoot, key)
		if err := os.MkdirAll(memberDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", memberDir, err)
		}
		gitCmd(t, memberDir, "init", "-b", "main")
		gitCmd(t, memberDir, "commit", "--allow-empty", "-m", "init")

		slug := key + "-slug"
		writeDoc(t, memberDir, "spec", slug,
			"# "+slug+"\n\n## Goal\n\n"+key+" work.\n", time.Now().Add(-time.Minute))

		wt := filepath.Join(memberDir, ".claude", "worktrees", key+"-wt")
		gitCmd(t, memberDir, "worktree", "add", wt, "-b", key+"-branch")
	}
	writeFile(t, filepath.Join(realmRoot, ".atomic", "code.toml"), toml)

	gitCmd(t, realmRoot, "init", "-b", "main")
	gitCmd(t, realmRoot, "commit", "--allow-empty", "-m", "init")

	return realmRoot, claudeMD
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A declared member with no code index still appears under ?member= —
// plansMembers deliberately omits discoverCodeMembers()'s code-index filter.
func TestPlansHandler_MemberWithNoCodeIndexAppears(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "unindexed-member")
	writeDoc(t, filepath.Join(realmRoot, "unindexed-member"), "spec", "member-slug",
		"# member-slug\n\n## Goal\n\nMember work.\n", time.Now().Add(-time.Minute))

	h := plansHandler(plansOptions{
		Root:         realmRoot,
		ScopeRoot:    realmRoot,
		ClaudeMDPath: claudeMD,
		Registry:     newPlansRegistry(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/plans?member=unindexed-member", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var rows []planRow
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	findRow(t, rows, "member-slug")
}

// A declared member's config key and its realm-relative prefix diverge
// whenever the path has a directory component (slugKey dedupes the key from
// the path's base name). The store sends the prefix; ?member= must still
// resolve for a client sending the old key.
func TestPlansHandler_MemberResolvesByPrefixWithKeyFallback(t *testing.T) {
	requireGit(t)
	realmRoot := t.TempDir()
	claudeMD := filepath.Join(realmRoot, "CLAUDE.md")
	writeFile(t, filepath.Join(realmRoot, "wiki", "index.md"), "# wiki\n")
	writeFile(t, claudeMD, "# CLAUDE.md\n\n<wikis>\n- "+filepath.Join(realmRoot, "wiki", "index.md")+"\n</wikis>\n")

	memberDir := filepath.Join(realmRoot, "repos", "api")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", memberDir, err)
	}
	gitCmd(t, memberDir, "init", "-b", "main")
	gitCmd(t, memberDir, "commit", "--allow-empty", "-m", "init")
	writeDoc(t, memberDir, "spec", "api-slug", "# api-slug\n\n## Goal\n\napi work.\n", time.Now().Add(-time.Minute))

	writeFile(t, filepath.Join(realmRoot, ".atomic", "code.toml"), "[[member]]\nkey = \"api\"\npath = \"repos/api\"\nexclude = false\n")

	gitCmd(t, realmRoot, "init", "-b", "main")
	gitCmd(t, realmRoot, "commit", "--allow-empty", "-m", "init")

	h := plansHandler(plansOptions{
		Root:         realmRoot,
		ScopeRoot:    realmRoot,
		ClaudeMDPath: claudeMD,
		Registry:     newPlansRegistry(),
	})

	for _, member := range []string{"repos/api", "api"} {
		req := httptest.NewRequest(http.MethodGet, "/api/plans?member="+member, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("member=%s: status = %d, want 200; body=%s", member, rr.Code, rr.Body.String())
		}
		var rows []planRow
		if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
			t.Fatalf("member=%s: unmarshal: %v; body=%s", member, err, rr.Body.String())
		}
		findRow(t, rows, "api-slug")
	}
}

// A page request for an id owned by one of three already-built aggregators
// spawns `git worktree list` once — resolving through that aggregator's own
// map — never once per aggregator in the registry.
func TestResolveWorktree_QueriesOnlyOwningAggregator(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	m1 := setupMainRepo(t, filepath.Join(root, "m1"))
	m2 := setupMainRepo(t, filepath.Join(root, "m2"))
	m3 := setupMainRepo(t, filepath.Join(root, "m3"))
	writeDoc(t, m2, "spec", "target", "# target\n\n## Goal\n\nFind me.\n", time.Now().Add(-time.Minute))

	reg := newPlansRegistry()
	rowsM2 := plansRows(t, reg, m2)
	plansRows(t, reg, m1)
	plansRows(t, reg, m3)

	row := findRow(t, rowsM2, "target")
	doc := findDoc(t, row, "docs/spec/target.md")
	id := doc.Versions[0].Checkouts[0].ID

	orig := runGitWorktreeList
	var calls int32
	runGitWorktreeList = func(dir string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return orig(dir)
	}
	defer func() { runGitWorktreeList = orig }()

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, id, "docs/spec/target.md", false)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("git worktree list spawned %d times for one page request, want 1", got)
	}
}

// The realm root is itself a pickable entry: N declared members yield N+1
// plansMembers rows, and one of them names the realm root's own path.
func TestPlansMembers_RealmRootIsItsOwnEntry(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "member-a", "member-b")

	members := plansMembers(realmRoot, claudeMD, "")
	if len(members) != 3 {
		t.Fatalf("len(members) = %d, want 3 (realm root + 2 declared members); got %+v", len(members), members)
	}

	foundRoot := false
	for _, m := range members {
		if m.Path == realmRoot && m.Key == "" {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Errorf("no member entry for the realm root itself in %+v", members)
	}
}

// allCheckoutIDs collects every checkout id appearing anywhere in a row set
// — across every doc's every version's every checkout — so a scoping test
// can assert two responses' worktree ids never overlap.
func allCheckoutIDs(rows []planRow) map[string]bool {
	ids := map[string]bool{}
	for _, r := range rows {
		for _, d := range r.Docs {
			for _, v := range d.Versions {
				for _, c := range v.Checkouts {
					ids[c.ID] = true
				}
			}
		}
	}
	return ids
}

func disjoint(t *testing.T, a, b map[string]bool, aName, bName string) {
	t.Helper()
	for id := range a {
		if b[id] {
			t.Errorf("worktree id %q present in both %s and %s responses", id, aName, bName)
		}
	}
}

// ?member=<key> aggregates exactly that member's own worktrees — never a
// union with its sibling member or the realm root — and the absent-member
// request aggregates the realm root's own rows, not a union of everything.
// CP16's core scoping proof.
func TestPlansHandler_MemberScopingIsExclusiveNeverAUnion(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "member-a", "member-b")
	writeDoc(t, realmRoot, "spec", "root-slug", "# root-slug\n\n## Goal\n\nRoot work.\n", time.Now().Add(-time.Minute))

	registry := newPlansRegistry()
	h := plansHandler(plansOptions{Root: realmRoot, ScopeRoot: realmRoot, ClaudeMDPath: claudeMD, Registry: registry})

	fetch := func(qs string) []planRow {
		req := httptest.NewRequest(http.MethodGet, "/api/plans"+qs, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /api/plans%s: status = %d, want 200; body=%s", qs, rr.Code, rr.Body.String())
		}
		var rows []planRow
		if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
			t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
		}
		return rows
	}
	hasSlug := func(rows []planRow, slug string) bool {
		for _, r := range rows {
			if r.Slug == slug {
				return true
			}
		}
		return false
	}

	rowsA := fetch("?member=member-a")
	if !hasSlug(rowsA, "member-a-slug") {
		t.Errorf("?member=member-a missing its own slug; rows=%+v", rowsA)
	}
	if hasSlug(rowsA, "member-b-slug") || hasSlug(rowsA, "root-slug") {
		t.Errorf("?member=member-a leaked another root's slug (union, not scoping); rows=%+v", rowsA)
	}

	rowsB := fetch("?member=member-b")
	if !hasSlug(rowsB, "member-b-slug") {
		t.Errorf("?member=member-b missing its own slug; rows=%+v", rowsB)
	}
	if hasSlug(rowsB, "member-a-slug") || hasSlug(rowsB, "root-slug") {
		t.Errorf("?member=member-b leaked another root's slug (union, not scoping); rows=%+v", rowsB)
	}

	rowsRoot := fetch("")
	if !hasSlug(rowsRoot, "root-slug") {
		t.Errorf("absent ?member missing the realm root's own slug; rows=%+v", rowsRoot)
	}
	if hasSlug(rowsRoot, "member-a-slug") || hasSlug(rowsRoot, "member-b-slug") {
		t.Errorf("absent ?member aggregated member slugs — must be the root's own rows, not a union; rows=%+v", rowsRoot)
	}

	idsA, idsB, idsRoot := allCheckoutIDs(rowsA), allCheckoutIDs(rowsB), allCheckoutIDs(rowsRoot)
	if len(idsA) == 0 || len(idsB) == 0 || len(idsRoot) == 0 {
		t.Fatalf("expected each response to carry at least one checkout id; A=%v B=%v root=%v", idsA, idsB, idsRoot)
	}
	disjoint(t, idsA, idsB, "member-a", "member-b")
	disjoint(t, idsA, idsRoot, "member-a", "root")
	disjoint(t, idsB, idsRoot, "member-b", "root")
}

// A page request resolves inside the worktree id's OWN root — never a
// sibling member's — even when the requested path exists there. The id's
// root is authoritative; the path is contained under it, or the request
// 404s.
func TestPlansPageHandler_IDIsScopedToItsOwnMemberRoot(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "member-a", "member-b")

	registry := newPlansRegistry()
	h := plansHandler(plansOptions{Root: realmRoot, ScopeRoot: realmRoot, ClaudeMDPath: claudeMD, Registry: registry})

	// Build member-a's aggregator (and its ids) via a real request.
	req := httptest.NewRequest(http.MethodGet, "/api/plans?member=member-a", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var rowsA []planRow
	if err := json.Unmarshal(rr.Body.Bytes(), &rowsA); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	rowA := findRow(t, rowsA, "member-a-slug")
	docA := findDoc(t, rowA, "docs/spec/member-a-slug.md")
	idA := docA.Versions[0].Checkouts[0].ID

	pageH := plansPageHandler(registry)

	// The own doc, through its own id, resolves.
	rrOK := plansPageRequest(t, pageH, idA, "docs/spec/member-a-slug.md", false)
	if rrOK.Code != http.StatusOK {
		t.Fatalf("own doc through own id: status = %d, want 200; body=%s", rrOK.Code, rrOK.Body.String())
	}

	// A path that exists in member-b but not member-a, requested through
	// member-a's id, must 404 — the id's root is authoritative.
	rrCross := plansPageRequest(t, pageH, idA, "docs/spec/member-b-slug.md", false)
	if rrCross.Code != http.StatusNotFound {
		t.Fatalf("cross-member path through member-a's id: status = %d, want 404; body=%s", rrCross.Code, rrCross.Body.String())
	}
}

// The realm root and each member resolve to distinct project keys — a
// realm of N member repos has N+1 distinct <project-key>s, so each root's
// reports, reminders, and archive are its own, never the realm's.
func TestPlansMembers_ProjectKeysDisjoint(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "member-a", "member-b")

	members := plansMembers(realmRoot, claudeMD, "")
	keyFor := func(k string) string {
		m, ok := findPlansMember(members, k)
		if !ok {
			t.Fatalf("no member entry for key %q in %+v", k, members)
		}
		return config.ProjectStateDir(m.Path)
	}

	rootKey := keyFor("")
	aKey := keyFor("member-a")
	bKey := keyFor("member-b")

	if rootKey == aKey || rootKey == bKey || aKey == bKey {
		t.Errorf("project keys not pairwise distinct: root=%q member-a=%q member-b=%q", rootKey, aKey, bKey)
	}
}

// A realm root with no .git anywhere (git worktree list fails there) still
// serves rows for it as the default ?member=-absent target.
func TestPlansHandler_NonGitRealmRootStillReturnsRows(t *testing.T) {
	realmRoot := t.TempDir()
	writeDoc(t, realmRoot, "spec", "y", "# y\n\n## Goal\n\nRoot work.\n", time.Now().Add(-time.Minute))

	h := plansHandler(plansOptions{Root: realmRoot, Registry: newPlansRegistry()})

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var rows []planRow
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	findRow(t, rows, "y")
}

func TestPlansMembersHandler_ExposesKeysNotPaths(t *testing.T) {
	requireGit(t)
	realmRoot, claudeMD := buildRealmFixture(t, "member-a", "member-b")

	h := plansMembersHandler(plansOptions{ScopeRoot: realmRoot, ClaudeMDPath: claudeMD})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/plans/members", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var resp plansMembersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Members) != 3 {
		t.Fatalf("len(members) = %d, want 3; got %+v", len(resp.Members), resp.Members)
	}
	if strings.Contains(rec.Body.String(), realmRoot) {
		t.Errorf("response leaks a filesystem path: %s", rec.Body.String())
	}
	foundRoot := false
	for _, m := range resp.Members {
		if m.Key == "" {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Errorf("realm root (empty key) absent from %+v", resp.Members)
	}
}
