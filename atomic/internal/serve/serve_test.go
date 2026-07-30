package serve_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
)

// syncBuffer is a goroutine-safe text buffer. startTestServer runs the server in
// a goroutine that writes its startup URL to opts.Stdout while the test goroutine
// polls String() for that line — a concurrent write+read that a plain
// strings.Builder does not allow. The mutex makes the harness race-free under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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

	var stdout syncBuffer
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
// TestRootRouteServesSPAShell verifies that GET / serves the embedded React
// shell's index.html (200, text/html) — the server no longer computes scope
// or a landing URL server-side; React Router and the /api/* fetches resolve
// the initial screen client-side.
func TestRootRouteServesSPAShell(t *testing.T) {
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
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
	if !strings.Contains(strings.ToLower(html), "<div id=\"root\">") && !strings.Contains(html, "main.js") {
		t.Errorf("expected the React shell's mount point or bundled entry script; got:\n%s", html)
	}
}

// TestUnmatchedNonAPIGETServesSPAShell verifies the cutover contract: every
// non-API, non-static, non-carried-JS-endpoint GET falls through to the SPA
// shell — including deep links like /page/<relpath> and legacy routes like
// /health — instead of 404. This is a documented accepted regression: React
// Router resolves the deep link client-side; there is no server-side 404 for
// these paths (only /api/* enforces its own 404s).
func TestUnmatchedNonAPIGETServesSPAShell(t *testing.T) {
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

	for _, path := range []string{"/health", "/page/README.md", "/graph", "/search", "/status", "/some/deep/link"} {
		resp, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: expected 200 (SPA fallback), got %d", path, resp.StatusCode)
		}
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("GET %s: expected text/html Content-Type, got %q", path, resp.Header.Get("Content-Type"))
		}
		if len(body) == 0 {
			t.Errorf("GET %s: empty SPA shell body", path)
		}
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
// TestCarriedAssetsServedFromEmbeddedDist verifies that the carried assets
// copied into frontend/dist at build time (app.css, graph-core.js) are served
// as real static files — not swallowed by the SPA index.html fallback.
func TestCarriedAssetsServedFromEmbeddedDist(t *testing.T) {
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

	resp, err := http.Get(baseURL + "/app.css")
	if err != nil {
		t.Fatalf("GET /app.css: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Error("app.css body is empty")
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "css") {
		t.Errorf("expected CSS Content-Type, got %q", ct)
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
