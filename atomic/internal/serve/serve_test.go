package serve_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// writeFile writes content to path, creating parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// buildClaudeMD writes a CLAUDE.md with a <wikis> block.
func buildClaudeMD(t *testing.T, claudeMDPath string, wikiIndexPaths []string) {
	t.Helper()
	block := "<wikis>\n"
	for _, p := range wikiIndexPaths {
		block += "- " + p + "\n"
	}
	block += "</wikis>\n"
	writeFile(t, claudeMDPath, "# CLAUDE.md\n\n"+block)
}

// buildCodeTOML writes a code.toml with the given members.
func buildCodeTOML(t *testing.T, realmRoot string, members []struct{ key, path string }) {
	t.Helper()
	var sb strings.Builder
	for _, m := range members {
		fmt.Fprintf(&sb, "[[member]]\nkey = %q\npath = %q\nexclude = false\n\n", m.key, m.path)
	}
	writeFile(t, filepath.Join(realmRoot, ".atomic", "code.toml"), sb.String())
}

// waitReady polls the URL until it responds or the deadline passes.
func waitReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready within %v", url, timeout)
}

// startTestServer starts a server on port 0 with the given options, waits
// until it prints its startup URL, and returns the base URL and a shutdown
// function. The caller must invoke shutdown() to cancel the context; the
// goroutine is reaped before shutdown() returns.
//
// opts.Port must be 0 (OS-assigned). opts.Stdout must be nil — this helper
// owns stdout so it can parse the URL line.
func startTestServer(t *testing.T, opts serve.Options) (baseURL string, shutdown func()) {
	t.Helper()
	if opts.Port != 0 {
		t.Fatal("startTestServer: opts.Port must be 0 (let OS pick)")
	}

	var stdout strings.Builder
	opts.Port = 0
	opts.Stdout = &stdout

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- serve.RunWithContext(ctx, opts)
	}()

	// Wait until stdout contains the URL line.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(stdout.String(), "\n") {
			if strings.HasPrefix(line, "http://127.0.0.1:") {
				baseURL = strings.TrimSpace(line)
				break
			}
		}
		if baseURL != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if baseURL == "" {
		cancel()
		<-done
		t.Fatalf("startTestServer: server did not print URL within 3s; stdout=%q", stdout.String())
	}

	shutdown = func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("startTestServer: server did not shut down within 5s of cancel()")
		}
	}
	return baseURL, shutdown
}

// TestHealthzReturns200 verifies the /healthz route returns 200 "ok".
// This proves the server binds, accepts connections, and routes correctly.
func TestHealthzReturns200(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if strings.TrimSpace(string(body)) != "ok" {
		t.Errorf("expected body %q, got %q", "ok", string(body))
	}
}

// TestPortZeroPicksFreePort verifies that --port 0 makes the server bind to an
// OS-assigned port and prints the actual chosen URL on stdout.
func TestPortZeroPicksFreePort(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	// URL must not be port 0 (OS replaced it with the actual port).
	if strings.HasSuffix(baseURL, ":0") {
		t.Errorf("server printed port 0 URL: %q", baseURL)
	}

	// Verify the server is actually listening on that URL.
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz at %s: %v", baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestRootRouteRendersShell verifies the / route returns HTML containing
// the Obsidian shell structure: top bar (breadcrumb + search + md|code toggle),
// left nav, middle content with [page|system] toggle, and right rail with 3 slots.
// The dead context-pane must be gone; #main-pane must NOT hx-get /health.
func TestRootRouteRendersShell(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Must be HTML.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}

	// Obsidian shell: nav-pane and main-pane must be present.
	for _, marker := range []string{"nav-pane", "main-pane"} {
		if !strings.Contains(html, marker) {
			t.Errorf("HTML missing %q landmark", marker)
		}
	}

	// Dead context-pane must be gone.
	if strings.Contains(html, "context-pane") {
		t.Error("HTML still contains dead 'context-pane' — must be replaced by right rail")
	}

	// Top bar: breadcrumb element.
	if !strings.Contains(html, "breadcrumb") {
		t.Error("HTML missing breadcrumb in top bar")
	}

	// Top bar: search box present.
	if !strings.Contains(html, `type="search"`) && !strings.Contains(html, "search-box") && !strings.Contains(html, `id="search"`) {
		t.Error("HTML missing search box in top bar")
	}

	// Top bar: md|code source toggle — assert by button IDs (removing the buttons
	// would break this, unlike substring checks on "md"/"code" that appear elsewhere).
	for _, id := range []string{"toggle-md", "toggle-code"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("HTML missing md|code toggle button with id=%q", id)
		}
	}

	// Middle content: [page|system] toggle — assert by button IDs (the substrings
	// "page" and "system" appear in other contexts, so ID matching is the real seam).
	for _, id := range []string{"mode-page", "mode-system"} {
		if !strings.Contains(html, `id="`+id+`"`) {
			t.Errorf("HTML missing page|system toggle button with id=%q", id)
		}
	}

	// Right rail: three stacked slots.
	for _, slot := range []string{"rail-graph", "rail-out", "rail-in"} {
		if !strings.Contains(html, slot) {
			t.Errorf("HTML missing right-rail slot %q", slot)
		}
	}

	// #main-pane must NOT hx-get /health — landing is a page view, not the health dashboard.
	if strings.Contains(html, `hx-get="/health"`) {
		t.Error("#main-pane hx-get must not be /health — landing must be the page view")
	}
}

// TestMainPaneLandingURL verifies that #main-pane's hx-get is a /page/ URL
// (the page view), not /health. For a bare repo with no wiki/README the server
// must still produce a /page/ landing URL (even if it's a generated overview).
func TestMainPaneLandingURL(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// main-pane must hx-get a /page/ URL, not /health.
	if !strings.Contains(html, `hx-get="/page/`) {
		t.Errorf("#main-pane must hx-get a /page/ URL; html snippet: %q",
			extractMainPaneSnippet(html))
	}
}

// TestMainPaneLandingURLRealmScope verifies that when the server is started with a
// realm-scope target (cwd is realm root, <wikis> block points at wiki/index.md under
// the realm root), #main-pane's hx-get is "/page/wiki/index.md" — not "/page/README.md".
func TestMainPaneLandingURLRealmScope(t *testing.T) {
	realmDir := t.TempDir()

	// Build the wiki/index.md that realm resolution expects.
	wikiIndex := filepath.Join(realmDir, "wiki", "index.md")
	writeFile(t, wikiIndex, "# wiki\n")

	// Write CLAUDE.md with a <wikis> block pointing at the wiki index.
	claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
	buildClaudeMD(t, claudeMD, []string{wikiIndex})

	// Write code.toml so realm.Resolve sees a real realm root.
	buildCodeTOML(t, realmDir, []struct{ key, path string }{
		{key: "repoA", path: "repos/repoA"},
	})

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    realmDir,
		ClaudeMDPath: claudeMD,
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Realm scope: #main-pane must hx-get the wiki index, not README.md.
	if !strings.Contains(html, `hx-get="/page/wiki/index.md"`) {
		t.Errorf("realm-scope #main-pane must hx-get /page/wiki/index.md; snippet: %q",
			extractMainPaneSnippet(html))
	}
}

// extractMainPaneSnippet returns the first ~200 chars around main-pane for diagnostics.
func extractMainPaneSnippet(html string) string {
	idx := strings.Index(html, "main-pane")
	if idx < 0 {
		return "(main-pane not found)"
	}
	start := idx - 20
	if start < 0 {
		start = 0
	}
	end := idx + 200
	if end > len(html) {
		end = len(html)
	}
	return html[start:end]
}

// TestStatusRouteReturns200 verifies that GET /status returns the health
// dashboard (200). The dashboard is demoted from landing to a reachable page.
func TestStatusRouteReturns200(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 from /status, got %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Error("/status returned empty body")
	}
	// Must render the health dashboard (not a redirect, not 404).
	if strings.Contains(string(body), "404") && !strings.Contains(string(body), "health") {
		t.Error("/status appears to be returning 404 content instead of health dashboard")
	}
}

// TestHealthRouteIsNoLongerTheLanding verifies that /health no longer serves
// the dashboard at the old route. The route is removed; /status is the new home.
func TestHealthRouteIsNoLongerTheLanding(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	// /health must return 404 — no longer registered.
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 from /health (route removed), got %d", resp.StatusCode)
	}
}

// TestScopeMappingTable exercises resolveDisplayScope against the four
// realm resolver scopes. It uses a real temp-dir setup injectable into the
// resolver — no hardcoded $HOME.
func TestScopeMappingTable(t *testing.T) {
	cases := []struct {
		name      string
		setup     func(t *testing.T) (cwd, claudeMDPath string)
		wantScope serve.DisplayScope
	}{
		{
			name: "bare repo no index → Repo",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				// No .claude/.atomic-index/atomic.db, no <wikis> block.
				claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
				writeFile(t, claudeMD, "# CLAUDE.md\n")
				return dir, claudeMD
			},
			wantScope: serve.DisplayScopeRepo,
		},
		{
			name: "local index present → Repo",
			setup: func(t *testing.T) (string, string) {
				dir := t.TempDir()
				// Write a stub db so Resolve sees it.
				dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")
				writeFile(t, dbPath, "stub")
				claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
				writeFile(t, claudeMD, "# CLAUDE.md\n")
				return dir, claudeMD
			},
			wantScope: serve.DisplayScopeRepo,
		},
		{
			name: "cwd is realm root → Realm",
			setup: func(t *testing.T) (string, string) {
				realmDir := t.TempDir()
				wikiIndex := filepath.Join(realmDir, "wiki", "index.md")
				writeFile(t, wikiIndex, "# wiki\n")
				claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
				buildClaudeMD(t, claudeMD, []string{wikiIndex})
				buildCodeTOML(t, realmDir, []struct{ key, path string }{
					{key: "repoA", path: "repos/repoA"},
				})
				return realmDir, claudeMD
			},
			wantScope: serve.DisplayScopeRealm,
		},
		{
			name: "cwd inside member → Member",
			setup: func(t *testing.T) (string, string) {
				realmDir := t.TempDir()
				wikiIndex := filepath.Join(realmDir, "wiki", "index.md")
				writeFile(t, wikiIndex, "# wiki\n")
				claudeMD := filepath.Join(t.TempDir(), "CLAUDE.md")
				buildClaudeMD(t, claudeMD, []string{wikiIndex})
				buildCodeTOML(t, realmDir, []struct{ key, path string }{
					{key: "repoA", path: "repos/repoA"},
				})
				memberDir := filepath.Join(realmDir, "repos", "repoA")
				if err := os.MkdirAll(memberDir, 0o755); err != nil {
					t.Fatal(err)
				}
				return memberDir, claudeMD
			},
			wantScope: serve.DisplayScopeMember,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cwd, claudeMDPath := tc.setup(t)
			got, err := serve.ResolveDisplayScope(cwd, claudeMDPath)
			if err != nil {
				t.Fatalf("ResolveDisplayScope: %v", err)
			}
			if got != tc.wantScope {
				t.Errorf("want %v, got %v", tc.wantScope, got)
			}
		})
	}
}

// TestOpenFlagNonFatalOnError verifies that a failing browser opener does not
// cause Run to return a non-zero exit code.
func TestOpenFlagNonFatalOnError(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         true, // opener will be injected as a stub that errors
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
		// BrowserOpener is the swappable seam: always returns an error.
		BrowserOpener: func(url string) error {
			return fmt.Errorf("fake: open failed")
		},
	})

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Server is up → opener error was non-fatal.
	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	shutdown()
}

// TestGracefulShutdownOnContextCancel verifies the server shuts down cleanly
// when the context is cancelled. We test via context cancellation, which is
// exactly what signal.NotifyContext bridges SIGINT to in production.
func TestGracefulShutdownOnContextCancel(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// shutdown() cancels the context and waits for the goroutine to exit,
	// asserting it does so within 5s.
	shutdown()

	// Confirm the server is no longer accepting connections.
	_, err := http.Get(baseURL + "/healthz")
	if err == nil {
		t.Error("expected connection refused after shutdown, got nil error")
	}
}

// TestStaticAssetsServedFromMemory verifies /static/vendor/htmx.min.js is
// served from embedded memory (Content-Type application/javascript, non-empty).
func TestStaticAssetsServedFromMemory(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         false,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
	})
	defer shutdown()

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/static/vendor/htmx.min.js")
	if err != nil {
		t.Fatalf("GET /static/vendor/htmx.min.js: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Error("htmx.min.js body is empty")
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected JS Content-Type, got %q", ct)
	}
}

// TestOpenFlagCalledWithURLOnSuccess verifies that when Open is true and the
// server starts successfully, the BrowserOpener seam receives the correct
// http://127.0.0.1:<actualPort> URL — confirming the right URL is passed, not
// a placeholder or port-0 value.
func TestOpenFlagCalledWithURLOnSuccess(t *testing.T) {
	dir := t.TempDir()

	// openerURL captures the URL the opener receives.
	// RunWithContext calls the opener synchronously before starting srv.Serve,
	// so it fires before startTestServer even finishes parsing stdout — but
	// startTestServer waits for the URL line, which is printed before the opener
	// is called, so by the time startTestServer returns, the opener has already fired.
	openerCh := make(chan string, 1)

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         true,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
		BrowserOpener: func(url string) error {
			select {
			case openerCh <- url:
			default:
				// opener called more than once — shouldn't happen
			}
			return nil
		},
	})
	defer shutdown()

	// The opener is called synchronously in RunWithContext before Serve starts.
	// startTestServer already waited for the URL line, which is printed before
	// the opener fires — so the channel should be ready or arrive immediately.
	var openerURL string
	select {
	case openerURL = <-openerCh:
	case <-time.After(3 * time.Second):
		t.Fatal("BrowserOpener was not called within 3s")
	}

	if openerURL != baseURL {
		t.Errorf("BrowserOpener received %q, want %q", openerURL, baseURL)
	}
	if strings.HasSuffix(openerURL, ":0") {
		t.Errorf("BrowserOpener received unresolved port-0 URL: %q", openerURL)
	}
}
