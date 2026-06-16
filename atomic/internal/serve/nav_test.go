package serve_test

// nav_test.go — CP3: nav tree tests (TDD, written before implementation).
//
// Covers:
//  1. Realm scope: six group labels present; member/concern/knowledge entries
//     carry /page/... hrefs; hx-get + hx-target="#main-pane" attributes set.
//  2. Repo/member scope (no wiki): docs file tree rendered instead of six groups.
//  3. Stale badge (seam): when staleness seam injects a stale member, badge appears.
//  4. Stale badge + bucket-diff badge (production): real filesystem triggers the
//     production computeStaleness path and proves badges render without seam injection.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildMinimalWikiRealm creates a temp realm dir with:
//   - wiki/index.md containing a <wiki-scan> block with 2 members
//   - wiki/concerns/foo.md
//   - wiki/knowledge/bar.md
//
// Returns the realm root.
func buildMinimalWikiRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(filepath.Join(wikiDir, "concerns"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "knowledge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "repos"), 0o755); err != nil {
		t.Fatal(err)
	}

	// wiki/index.md with a <wiki-scan> block listing 2 members.
	indexContent := `<wiki-scan root="/realm" generated="2026-01-01">
<repo path="alpha" status="summarized" summary="repos/alpha.md"/>
<repo path="beta" status="pending"/>
</wiki-scan>

## Realm overview

Some narrative.
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	// wiki/concerns/foo.md
	writeFile(t, filepath.Join(wikiDir, "concerns", "foo.md"), "# Foo concern\n")

	// wiki/knowledge/bar.md
	writeFile(t, filepath.Join(wikiDir, "knowledge", "bar.md"), "# Bar knowledge\n")

	return root
}

// buildRepoScope creates a temp dir with some docs files and no wiki/.
func buildRepoScope(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "README.md"), "# Readme\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "# Guide\n")
	writeFile(t, filepath.Join(root, "docs", "api.md"), "# API\n")

	return root
}

// TestNavTreeRealmScopeGroupLabels verifies that the nav tree for a realm scope
// contains all six group labels: Realm, Repos, Concerns, Knowledge, Buckets, External.
func TestNavTreeRealmScopeGroupLabels(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// All six group labels must appear.
	for _, label := range []string{"Realm", "Repos", "Concerns", "Knowledge", "Buckets", "External"} {
		if !strings.Contains(body, label) {
			t.Errorf("nav tree missing group label %q", label)
		}
	}
}

// TestNavTreeRealmScopeMemberEntries verifies that member, concern, and knowledge
// entries appear in the nav tree with correct /page/ hrefs.
func TestNavTreeRealmScopeMemberEntries(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Member entries: "alpha" and "beta" should appear.
	if !strings.Contains(body, "alpha") {
		t.Error("nav tree missing member 'alpha'")
	}
	if !strings.Contains(body, "beta") {
		t.Error("nav tree missing member 'beta'")
	}

	// Concern entry: "foo" should appear.
	if !strings.Contains(body, "foo") {
		t.Error("nav tree missing concern 'foo'")
	}

	// Knowledge entry: "bar" should appear.
	if !strings.Contains(body, "bar") {
		t.Error("nav tree missing knowledge 'bar'")
	}
}

// TestNavTreeLeafHtmxAttributes verifies that nav leaves carry the required
// htmx attributes: hx-get="/page/..." and hx-target="#main-pane".
func TestNavTreeLeafHtmxAttributes(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// Must contain hx-get="/page/ somewhere.
	if !strings.Contains(body, `hx-get="/page/`) {
		t.Error("nav leaf missing hx-get=\"/page/...\" attribute")
	}

	// Must contain hx-target="#main-pane".
	if !strings.Contains(body, `hx-target="#main-pane"`) {
		t.Error("nav leaf missing hx-target=\"#main-pane\" attribute")
	}

	// Must contain hx-push-url="true".
	if !strings.Contains(body, `hx-push-url="true"`) {
		t.Error("nav leaf missing hx-push-url=\"true\" attribute")
	}
}

// TestNavTreeRealmIndexLink verifies that the Realm group links to wiki/index.md
// via /page/.
func TestNavTreeRealmIndexLink(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// The realm index link should point to wiki/index.md via /page/.
	if !strings.Contains(body, `/page/wiki/index.md`) {
		t.Errorf("nav tree missing realm index link /page/wiki/index.md; body excerpt:\n%s",
			body[:min(len(body), 500)])
	}
}

// TestNavTreeRepoScope verifies that a repo scope (no wiki) renders a docs
// file tree instead of the six realm groups.
func TestNavTreeRepoScope(t *testing.T) {
	root := buildRepoScope(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// Should NOT contain realm-specific group labels.
	for _, label := range []string{"Concerns", "Knowledge", "Buckets", "External"} {
		if strings.Contains(body, label) {
			t.Errorf("repo-scope nav unexpectedly contains realm group %q", label)
		}
	}

	// Should contain docs files: guide, api, README.
	if !strings.Contains(body, "README") {
		t.Error("repo-scope nav missing README.md")
	}
	if !strings.Contains(body, "guide") {
		t.Error("repo-scope nav missing docs/guide.md")
	}
	if !strings.Contains(body, "api") {
		t.Error("repo-scope nav missing docs/api.md")
	}
}

// TestNavTreeStaleMemberBadge verifies that when a member is flagged stale via
// the StalenessFn seam, a stale badge appears in the nav.
func TestNavTreeStaleMemberBadge(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
		// Staleness seam: inject "alpha" as stale via the injectable function.
		StalenessFn: func(_, _ string) (map[string]bool, map[string]bool) {
			return map[string]bool{"alpha": true}, map[string]bool{}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// A stale badge must appear near "alpha".
	if !strings.Contains(body, "stale") && !strings.Contains(body, "STALE") {
		t.Error("expected stale badge in nav tree for member 'alpha'")
	}
}

// TestNavTreeProductionStalenessPath verifies that the production path (no seam
// injection) correctly computes staleness from the filesystem and renders both a
// stale badge on a member and a diff badge on a bucket.
//
// Staleness is triggered without git by writing a <wiki-scan> block that lists
// a member ("ghost") whose directory does not exist — wiki.Stale reports
// "DRIFT removed ghost", which computeStaleness maps to staleMembers["ghost"]=true.
//
// The bucket diff is triggered by registering a bucket whose baseline manifest is
// empty while the live bucket directory contains a file — all files appear as Added,
// so wiki.Stale emits "STALE bucket research", causing bucketDiffs["research"]=true.
func TestNavTreeProductionStalenessPath(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")

	// Create required subdirectories.
	for _, sub := range []string{"concerns", "knowledge", "repos", ".buckets/research"} {
		if err := os.MkdirAll(filepath.Join(wikiDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create the bucket dir (sibling of wiki/) and put a file in it.
	bucketDir := filepath.Join(root, "research")
	if err := os.MkdirAll(bucketDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bucketDir, "note.md"), "# Research note\n")

	// Write an EMPTY baseline so all files in the bucket appear as Added.
	// wiki.Stale calls bucketDiffReadOnly which reads this baseline.
	writeFile(t, filepath.Join(wikiDir, ".buckets", "research", "baseline"), "")

	// wiki/index.md: list "ghost" as a member (but ghost/ dir does not exist
	// → DRIFT removed ghost) and register "research" as a bucket.
	researchAbsPath := bucketDir
	indexContent := `<wiki-scan root="` + root + `" generated="2026-01-01">
<repo path="ghost" status="pending"/>
</wiki-scan>

<wiki-buckets>
<bucket name="research" path="` + researchAbsPath + `"/>
</wiki-buckets>
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	// Use the production handler with NO StalenessFn injection.
	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(wikiDir, "index.md"),
		// StalenessFn is nil → production computeStaleness fires.
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// The stale badge must appear (triggered by DRIFT removed ghost).
	if !strings.Contains(body, "stale") && !strings.Contains(body, "STALE") {
		t.Error("production path: expected stale badge for drifted member 'ghost'")
	}

	// The diff badge must appear (triggered by STALE bucket research).
	if !strings.Contains(body, "diff") {
		t.Error("production path: expected diff badge for bucket 'research'")
	}
}

// TestNavTreeFolderTreeDepth2 verifies that a docs layout with files at two
// levels of subdirectory nesting (docs/a/b/c.md and docs/a/d.md) renders as a
// true recursive tree: "b" nested under "a", "c" nested under "b", with the
// leaf's hx-get pointing to the full path /page/docs/a/b/c.md.
//
// This is the contract that rules out the old flat-grouping implementation
// which collapsed docs/a/b/c.md and docs/a/d/c.md into the same "a" folder
// using only filepath.Base labels.
func TestNavTreeFolderTreeDepth2(t *testing.T) {
	root := t.TempDir()

	// docs/a/b/c.md — depth 2 under docs/
	// docs/a/d.md   — depth 1 under docs/a/
	writeFile(t, filepath.Join(root, "docs", "a", "b", "c.md"), "# C\n")
	writeFile(t, filepath.Join(root, "docs", "a", "d.md"), "# D\n")

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:    root,
		IsRealmScope: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	body := rr.Body.String()

	// The leaf for c.md must link to the FULL path, not just the base name.
	if !strings.Contains(body, `/page/docs/a/b/c.md`) {
		t.Errorf("nav tree missing full-path leaf for docs/a/b/c.md; body:\n%s", body)
	}

	// The "b" folder <details> must exist and contain the c.md leaf.
	bIdx := strings.Index(body, `>b<`)
	if bIdx == -1 {
		// Try the summary element form with the exact text.
		bIdx = strings.Index(body, ">b</summary>")
	}
	if bIdx == -1 {
		t.Fatalf("nav tree missing folder summary for 'b'; body:\n%s", body)
	}
	cIdx := strings.Index(body, `/page/docs/a/b/c.md`)
	if cIdx == -1 {
		t.Fatalf("leaf /page/docs/a/b/c.md not found; body:\n%s", body)
	}
	// "b" summary must appear BEFORE the c.md leaf (it wraps it).
	if bIdx >= cIdx {
		t.Errorf("expected 'b' folder to appear before c.md leaf; b at %d, c at %d", bIdx, cIdx)
	}

	// The "a" folder <details> must also contain the "b" <details>.
	aIdx := strings.Index(body, ">a</summary>")
	if aIdx == -1 {
		t.Fatalf("nav tree missing folder summary for 'a'; body:\n%s", body)
	}
	if aIdx >= bIdx {
		t.Errorf("expected 'a' folder to appear before 'b' folder; a at %d, b at %d", aIdx, bIdx)
	}

	// d.md is a sibling of b/ inside a/ — must also appear with its full path.
	if !strings.Contains(body, `/page/docs/a/d.md`) {
		t.Errorf("nav tree missing full-path leaf for docs/a/d.md; body:\n%s", body)
	}
}

// TestNavTreeExternalLink verifies the External group contains a link to /external.
func TestNavTreeExternalLink(t *testing.T) {
	root := buildMinimalWikiRealm(t)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(root, "wiki", "index.md"),
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// External group must link to /external.
	if !strings.Contains(body, "/external") {
		t.Error("nav tree missing /external link in External group")
	}
}

// TestNavTreeMemberLinksMirrorIndex verifies that nav member links match the
// wiki index's classification: indexed → its signals page, pending → its
// directory. It must NEVER emit a guessed wiki/repos/<name>.md link, which is
// the bug that 404'd every repo in the left nav (summary files don't exist on
// disk for indexed/pending members).
func TestNavTreeMemberLinksMirrorIndex(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	indexedSignals := filepath.Join(root, "alpha", ".claude", "project", "signals.md")
	indexContent := `<wiki-scan root="` + root + `" generated="2026-01-01">
<repo path="alpha" status="indexed" signals="` + indexedSignals + `"/>
<repo path="beta" status="pending"/>
</wiki-scan>
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(wikiDir, "index.md"),
		StalenessFn: func(_, _ string) (map[string]bool, map[string]bool) {
			return map[string]bool{}, map[string]bool{}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	body := rr.Body.String()

	// indexed member → its signals page (realm-relative).
	if !strings.Contains(body, "/page/alpha/.claude/project/signals.md") {
		t.Errorf("indexed member must link to its signals page, got:\n%s", body)
	}
	// pending member → its directory (served as a folder index/listing).
	if !strings.Contains(body, `/page/beta/"`) {
		t.Errorf("pending member must link to its directory /page/beta/, got:\n%s", body)
	}
	// never the guessed, nonexistent wiki/repos path.
	if strings.Contains(body, "wiki/repos/") {
		t.Errorf("nav must not emit guessed wiki/repos/ links, got:\n%s", body)
	}
}

// TestNavTreeBucketFolderIsBrowsable verifies that a registered capture bucket
// renders its markdown files as clickable /page/<bucket>/<file> links (a
// browsable folder) rather than a dead, non-clickable span.
func TestNavTreeBucketFolderIsBrowsable(t *testing.T) {
	root := t.TempDir()
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A capture bucket (realm-root sibling of wiki/) holding a markdown file.
	ticketsDir := filepath.Join(root, "tickets")
	if err := os.MkdirAll(ticketsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(ticketsDir, "007-search-ui.md"), "# Ticket 007\n")

	indexContent := `<wiki-scan root="` + root + `" generated="2026-01-01">
</wiki-scan>

<wiki-buckets>
<bucket name="tickets" path="` + ticketsDir + `"/>
</wiki-buckets>
`
	writeFile(t, filepath.Join(wikiDir, "index.md"), indexContent)

	handler := serve.NewNavHandler(serve.NavOptions{
		RealmRoot:     root,
		IsRealmScope:  true,
		WikiIndexPath: filepath.Join(wikiDir, "index.md"),
		// No staleness I/O for this assertion.
		StalenessFn: func(_, _ string) (map[string]bool, map[string]bool) {
			return map[string]bool{}, map[string]bool{}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/nav", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	body := rr.Body.String()

	// The bucket's markdown file must be a clickable /page/ link.
	if !strings.Contains(body, "/page/tickets/007-search-ui.md") {
		t.Errorf("expected browsable bucket file link /page/tickets/007-search-ui.md in nav, got:\n%s", body)
	}
	// And it must not be a dead non-clickable span (old behavior).
	if strings.Contains(body, `nav-leaf`) {
		t.Errorf("bucket should no longer render a dead nav-leaf span, got:\n%s", body)
	}
}
