package serve_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// buildExternalRealm creates a temp realm with two pages for external-link tests.
//
//	pageA.md — links to https://example.com/x and http://foo.test
//	pageB.md — also links to https://example.com/x
//	pageC.md — only internal/wikilinks + a link inside a fenced code block
func buildExternalRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pageA.md"),
		"# A\n\nSee [example](https://example.com/x) and [foo](http://foo.test).\n")
	writeFile(t, filepath.Join(root, "pageB.md"),
		"# B\n\nAlso see [example](https://example.com/x).\n")
	writeFile(t, filepath.Join(root, "pageC.md"),
		"# C\n\nInternal [link](pageA.md) and wikilink [[pageA]].\n\n```\nhttps://inside-code.test/should-be-excluded\n```\n")

	return root
}

// fixedDateFn returns a FileDateFn that always returns the given date for any file.
func fixedDateFn(d time.Time) serve.FileDateFn {
	return func(_ string) time.Time {
		return d
	}
}

// perFileDateFn returns a FileDateFn that maps abs path → time, falling back to epoch.
func perFileDateFn(m map[string]time.Time) serve.FileDateFn {
	return func(p string) time.Time {
		if t, ok := m[p]; ok {
			return t
		}
		return time.Time{}
	}
}

// TestExternalRegistry_UniqueURLs verifies the registry collects exactly the
// external URLs present in the realm (http/https only) and deduplicates them.
func TestExternalRegistry_UniqueURLs(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	if len(reg) != 2 {
		t.Errorf("expected 2 unique external URLs, got %d: %v", len(reg), reg)
	}

	found := map[string]bool{}
	for _, e := range reg {
		found[e.URL] = true
	}
	if !found["https://example.com/x"] {
		t.Error("expected https://example.com/x in registry")
	}
	if !found["http://foo.test"] {
		t.Error("expected http://foo.test in registry")
	}
}

// TestExternalRegistry_SourcePages verifies that https://example.com/x lists
// both pageA.md and pageB.md as sources.
func TestExternalRegistry_SourcePages(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	var entry *serve.ExternalEntry
	for i := range reg {
		if reg[i].URL == "https://example.com/x" {
			entry = &reg[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("https://example.com/x not found in registry")
	}

	sources := map[string]bool{}
	for _, s := range entry.Sources {
		sources[s] = true
	}
	if !sources["pageA.md"] {
		t.Errorf("expected pageA.md in sources for https://example.com/x, got %v", entry.Sources)
	}
	if !sources["pageB.md"] {
		t.Errorf("expected pageB.md in sources for https://example.com/x, got %v", entry.Sources)
	}
}

// TestExternalRegistry_InternalLinksExcluded verifies that relative (internal)
// markdown links and wikilinks are NOT in the registry.
func TestExternalRegistry_InternalLinksExcluded(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	for _, e := range reg {
		if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
			t.Errorf("non-http URL in registry: %q", e.URL)
		}
		if strings.Contains(e.URL, "pageA") {
			t.Errorf("internal link leaked into registry: %q", e.URL)
		}
	}
}

// TestExternalRegistry_FencedCodeExcluded verifies that a URL inside a fenced
// code block in pageC.md is excluded. ExtractLinks handles this — this test
// asserts the end-to-end behavior at the registry level.
func TestExternalRegistry_FencedCodeExcluded(t *testing.T) {
	root := buildExternalRealm(t)
	reg := serve.BuildExternalRegistry(root, fixedDateFn(time.Now()))

	for _, e := range reg {
		if strings.Contains(e.URL, "inside-code.test") {
			t.Errorf("URL inside fenced code block leaked into registry: %q", e.URL)
		}
	}
}

// TestExternalRegistry_FirstSeenUsesDateSeam verifies that the first-seen date
// is driven by the injected FileDateFn (the seam), not the actual file system.
func TestExternalRegistry_FirstSeenUsesDateSeam(t *testing.T) {
	root := buildExternalRealm(t)

	dateA := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	dateB := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	// pageA.md cites https://example.com/x with dateA; pageB.md also cites it with dateB.
	// first-seen should be the EARLIEST = dateA.
	pathA := filepath.Join(root, "pageA.md")
	pathB := filepath.Join(root, "pageB.md")
	pathC := filepath.Join(root, "pageC.md")

	dateFn := perFileDateFn(map[string]time.Time{
		pathA: dateA,
		pathB: dateB,
		pathC: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	})

	reg := serve.BuildExternalRegistry(root, dateFn)

	var entry *serve.ExternalEntry
	for i := range reg {
		if reg[i].URL == "https://example.com/x" {
			entry = &reg[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("https://example.com/x not found in registry")
	}

	if !entry.FirstSeen.Equal(dateA) {
		t.Errorf("expected first-seen %v (earliest), got %v", dateA, entry.FirstSeen)
	}
}

// TestExternalHandler_Returns200 verifies that GET /external returns 200 with
// the registry table rendered (URLs present in the body).
func TestExternalHandler_Returns200(t *testing.T) {
	root := buildExternalRealm(t)
	handler := serve.NewExternalHandler(root, fixedDateFn(time.Now()))

	req := httptest.NewRequest(http.MethodGet, "/external", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Both external URLs must appear in the rendered output.
	if !strings.Contains(html, "https://example.com/x") {
		t.Errorf("https://example.com/x missing from /external response:\n%s", html)
	}
	if !strings.Contains(html, "http://foo.test") {
		t.Errorf("http://foo.test missing from /external response:\n%s", html)
	}
}

// TestExternalHandler_SourceLinksToPage verifies that source pages are rendered
// as links to /page/<relpath>.
func TestExternalHandler_SourceLinksToPage(t *testing.T) {
	root := buildExternalRealm(t)
	handler := serve.NewExternalHandler(root, fixedDateFn(time.Now()))

	req := httptest.NewRequest(http.MethodGet, "/external", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	html := string(body)

	// Source pages must link to /page/<relpath>.
	if !strings.Contains(html, "/page/pageA.md") {
		t.Errorf("expected /page/pageA.md link in /external response:\n%s", html)
	}
	if !strings.Contains(html, "/page/pageB.md") {
		t.Errorf("expected /page/pageB.md link in /external response:\n%s", html)
	}
}

// TestExternalHandler_HTMXFragment verifies that HX-Request returns a fragment
// (no full DOCTYPE) suitable for htmx swap into #main-pane.
func TestExternalHandler_HTMXFragment(t *testing.T) {
	root := buildExternalRealm(t)
	handler := serve.NewExternalHandler(root, fixedDateFn(time.Now()))

	req := httptest.NewRequest(http.MethodGet, "/external", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for htmx fragment, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Fragment must NOT include the full HTML shell.
	if strings.Contains(html, "<!DOCTYPE") {
		t.Errorf("htmx fragment must not include DOCTYPE:\n%s", html)
	}
	// But must still contain the URL data.
	if !strings.Contains(html, "https://example.com/x") {
		t.Errorf("https://example.com/x missing from htmx fragment:\n%s", html)
	}
}

// TestExternalHandler_DateRendered verifies that first-seen dates are rendered in the response.
func TestExternalHandler_DateRendered(t *testing.T) {
	root := buildExternalRealm(t)
	fixed := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	handler := serve.NewExternalHandler(root, fixedDateFn(fixed))

	req := httptest.NewRequest(http.MethodGet, "/external", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body, _ := io.ReadAll(w.Result().Body)
	html := string(body)

	// The date "2024-05-01" must appear somewhere in the output.
	if !strings.Contains(html, "2024-05-01") {
		t.Errorf("expected date 2024-05-01 in /external response:\n%s", html)
	}
}

// ─── GitOrMtimeDateFn tests ───────────────────────────────────────────────────

// initGitRepo initialises a fresh git repo in dir with a minimal identity and
// returns the dir. Uses only local git config (--local) so it never mutates the
// user's global identity.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "--local", "user.email", "test@example.com")
	run("config", "--local", "user.name", "Test")
}

// TestGitOrMtimeDateFn_CommittedFile verifies that GitOrMtimeDateFn returns the
// git add-date (the date the file was first committed) when git is available and
// the file is tracked. The add-date is pinned via GIT_AUTHOR_DATE /
// GIT_COMMITTER_DATE env vars so the assertion is deterministic.
func TestGitOrMtimeDateFn_CommittedFile(t *testing.T) {
	// Skip if git is not available on PATH.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Write and commit a file with a fixed author/committer date.
	filePath := filepath.Join(dir, "committed.md")
	writeFile(t, filePath, "# Committed\n")

	pinDate := "2023-07-04T12:00:00+00:00" // RFC3339; git will echo this back
	cmd := exec.Command("git", "add", "committed.md")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "add committed.md")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+pinDate,
		"GIT_COMMITTER_DATE="+pinDate,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	want, _ := time.Parse(time.RFC3339, pinDate)
	got := serve.GitOrMtimeDateFn(filePath)

	// GitOrMtimeDateFn must return the pinned add-date, not the file's mtime.
	if !got.Equal(want) {
		t.Errorf("GitOrMtimeDateFn: want %v (git add-date), got %v", want, got)
	}
}

// TestGitOrMtimeDateFn_NonGitDir verifies that GitOrMtimeDateFn falls back to
// mtime when called on a file in a directory that is not a git repository.
func TestGitOrMtimeDateFn_NonGitDir(t *testing.T) {
	// Skip if git is not available on PATH.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	// Note: no git init — this is intentionally NOT a git repo.
	filePath := filepath.Join(dir, "plain.md")
	writeFile(t, filePath, "# Plain\n")

	before := time.Now().Add(-time.Second)
	got := serve.GitOrMtimeDateFn(filePath)
	after := time.Now().Add(time.Second)

	// The returned time must be within [before, after] — i.e. the file's mtime.
	if got.Before(before) || got.After(after) {
		t.Errorf("GitOrMtimeDateFn in non-git dir: got %v, expected mtime in [%v, %v]", got, before, after)
	}
}

// TestGitOrMtimeDateFn_UntrackedFile verifies that GitOrMtimeDateFn falls back
// to mtime when the file exists in a git repo but has never been committed
// (git log --diff-filter=A produces empty output for untracked files).
func TestGitOrMtimeDateFn_UntrackedFile(t *testing.T) {
	// Skip if git is not available on PATH.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	dir := t.TempDir()
	initGitRepo(t, dir)

	// Write the file but do NOT add or commit it — it stays untracked.
	filePath := filepath.Join(dir, "untracked.md")
	writeFile(t, filePath, "# Untracked\n")

	before := time.Now().Add(-time.Second)
	got := serve.GitOrMtimeDateFn(filePath)
	after := time.Now().Add(time.Second)

	// Must fall back to mtime since the file has no git history.
	if got.Before(before) || got.After(after) {
		t.Errorf("GitOrMtimeDateFn for untracked file: got %v, expected mtime in [%v, %v]", got, before, after)
	}
}
