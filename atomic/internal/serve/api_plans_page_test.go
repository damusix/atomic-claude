package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/scratchpad"
)

// plansRows drives plansHandler and decodes the rows, so a test can pull a
// real worktree id off a real response rather than reaching into the
// registry's internals.
func plansRows(t *testing.T, reg *plansRegistry, root string) []planRow {
	t.Helper()
	h := plansHandler(plansOptions{Root: root, Registry: reg})
	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("plansHandler status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var rows []planRow
	if err := json.Unmarshal(rr.Body.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	return rows
}

func plansPageRequest(t *testing.T, h http.Handler, worktree, path string, raw bool) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	q.Set("worktree", worktree)
	q.Set("path", path)
	if raw {
		q.Set("raw", "1")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/plans/page?"+q.Encode(), nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// A file under a worktree outside the served root is servable by id, even
// though /api/page would refuse it (safeResolve is scoped to root).
func TestPlansPageHandler_ServesFileFromNonServedRootWorktree(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	outsideParent := t.TempDir()
	outsideWt := filepath.Join(outsideParent, "elsewhere")
	gitCmd(t, main, "worktree", "add", outsideWt, "-b", "outside-branch")
	writeDoc(t, outsideWt, "design", "far", "# far\n\n## Goal\n\nWork done elsewhere.\n", time.Now().Add(-time.Minute))

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "far")
	doc := findDoc(t, row, "docs/design/far.md")
	id := doc.Versions[0].Checkouts[0].ID

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, id, "docs/design/far.md", false)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var got apiPageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.RelPath != "docs/design/far.md" {
		t.Errorf("RelPath = %q, want docs/design/far.md", got.RelPath)
	}
	if got.HTML == "" {
		t.Error("HTML: got empty, want rendered body")
	}
}

func TestPlansPageHandler_UnknownIDRejected(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	reg := newPlansRegistry()
	plansRows(t, reg, main) // build at least one aggregator so resolveWorktree has something to search

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, "does-not-exist", "README.md", false)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// A worktree removed after the id was issued must be rejected, not resolved
// against a stale cached map.
func TestPlansPageHandler_StaleIDRejected(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)

	outsideParent := t.TempDir()
	outsideWt := filepath.Join(outsideParent, "elsewhere")
	gitCmd(t, main, "worktree", "add", outsideWt, "-b", "outside-branch")
	writeDoc(t, outsideWt, "design", "gone", "# gone\n\n## Goal\n\nWill be removed.\n", time.Now().Add(-time.Minute))

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "gone")
	doc := findDoc(t, row, "docs/design/gone.md")
	id := doc.Versions[0].Checkouts[0].ID

	gitCmd(t, main, "worktree", "remove", "--force", outsideWt)

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, id, "docs/design/gone.md", false)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a removed worktree's id; body=%s", rr.Code, rr.Body.String())
	}
}

// raw=1 on an html bundle file serves the file's own content-type and raw
// bytes, rather than the rendered HTML-in-JSON /api/page shape.
func TestPlansPageHandler_RawHTMLBundleFile(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	bundle, _, err := scratchpad.New(main, "visual-slug", "plan")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	htmlContent := "<html><body>options</body></html>"
	if err := os.WriteFile(filepath.Join(bundle.Root, "options.html"), []byte(htmlContent), 0o644); err != nil {
		t.Fatalf("write options.html: %v", err)
	}

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "visual-slug")
	if len(row.Bundles) != 1 {
		t.Fatalf("bundles = %+v, want 1", row.Bundles)
	}
	worktreeID := row.Bundles[0].WorktreeID

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, worktreeID, ".claude/.scratchpad/visual-slug/options.html", true)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
	if rr.Body.String() != htmlContent {
		t.Errorf("body = %q, want raw bytes %q", rr.Body.String(), htmlContent)
	}
}

// No raw param on a markdown doc renders the same apiPageResponse shape
// /api/page returns.
func TestPlansPageHandler_NoRawMarkdownDoc(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	writeDoc(t, main, "spec", "rendered", "# rendered\n\n## Goal\n\nRender me.\n", time.Now().Add(-time.Minute))

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "rendered")
	doc := findDoc(t, row, "docs/spec/rendered.md")
	id := doc.Versions[0].Checkouts[0].ID

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, id, "docs/spec/rendered.md", false)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got apiPageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rr.Body.String())
	}
	if got.HTML == "" {
		t.Error("HTML: got empty, want rendered body")
	}
}

// A bundle file classified "file" (by extension) whose bytes open an HTML
// signature must not be served as text/html — the classification is a
// floor, not something a content sniff can override.
func TestPlansPageHandler_RawFileKindNeverSniffedToHTML(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	bundle, _, err := scratchpad.New(main, "html-in-txt", "plan")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	evil := "<html><script>alert(1)</script></html>"
	if err := os.WriteFile(filepath.Join(bundle.Root, "notes.txt"), []byte(evil), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "html-in-txt")
	if len(row.Bundles) != 1 {
		t.Fatalf("bundles = %+v, want 1", row.Bundles)
	}
	worktreeID := row.Bundles[0].WorktreeID

	h := plansPageHandler(reg)
	rr := plansPageRequest(t, h, worktreeID, ".claude/.scratchpad/html-in-txt/notes.txt", true)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, a kind=file bundle file must never be sniffed to text/html", ct)
	}
}

// Every raw=1 response — html, markdown, and file kinds — carries a
// response-level sandbox CSP, since the URL is reachable by direct
// navigation and shared link, bypassing the page's iframe sandbox entirely.
func TestPlansPageHandler_RawResponsesCarrySandboxCSP(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	restoreHome := config.SetHomeDirForTest(t.TempDir())
	defer restoreHome()

	bundle, _, err := scratchpad.New(main, "csp-slug", "plan")
	if err != nil {
		t.Fatalf("scratchpad.New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle.Root, "options.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write options.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle.Root, "notes.txt"), []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle.Root, "BRIEF.md"), []byte("# brief\n"), 0o644); err != nil {
		t.Fatalf("write BRIEF.md: %v", err)
	}

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "csp-slug")
	if len(row.Bundles) != 1 {
		t.Fatalf("bundles = %+v, want 1", row.Bundles)
	}
	worktreeID := row.Bundles[0].WorktreeID

	h := plansPageHandler(reg)
	for _, relPath := range []string{
		".claude/.scratchpad/csp-slug/options.html",
		".claude/.scratchpad/csp-slug/notes.txt",
		".claude/.scratchpad/csp-slug/BRIEF.md",
	} {
		rr := plansPageRequest(t, h, worktreeID, relPath, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("path=%q: status = %d, want 200; body=%s", relPath, rr.Code, rr.Body.String())
		}
		if csp := rr.Header().Get("Content-Security-Policy"); csp != "sandbox" {
			t.Errorf("path=%q: Content-Security-Policy = %q, want %q", relPath, csp, "sandbox")
		}
	}
}

// path=../../etc/passwd and path=/etc/passwd are both rejected, even for a
// worktree id that resolves cleanly.
func TestPlansPageHandler_PathTraversalRejected(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	main := setupMainRepo(t, root)
	writeDoc(t, main, "spec", "anchor", "# anchor\n\n## Goal\n\nAnchors the fixture.\n", time.Now().Add(-time.Minute))

	reg := newPlansRegistry()
	rows := plansRows(t, reg, main)
	row := findRow(t, rows, "anchor")
	doc := findDoc(t, row, "docs/spec/anchor.md")
	id := doc.Versions[0].Checkouts[0].ID

	h := plansPageHandler(reg)

	for _, p := range []string{"../../etc/passwd", "/etc/passwd"} {
		rr := plansPageRequest(t, h, id, p, false)
		if rr.Code != http.StatusNotFound {
			t.Errorf("path=%q: status = %d, want 404; body=%s", p, rr.Code, rr.Body.String())
		}
	}
}
