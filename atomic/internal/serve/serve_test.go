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

// The server goroutine writes its startup URL while the test goroutine polls for
// it — a concurrent write+read a plain strings.Builder trips over under -race.
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func buildClaudeMD(t *testing.T, claudeMDPath string, wikiIndexPaths []string) {
	t.Helper()
	block := "<wikis>\n"
	for _, p := range wikiIndexPaths {
		block += "- " + p + "\n"
	}
	block += "</wikis>\n"
	writeFile(t, claudeMDPath, "# CLAUDE.md\n\n"+block)
}

func buildCodeTOML(t *testing.T, realmRoot string, members []struct{ key, path string }) {
	t.Helper()
	var sb strings.Builder
	for _, m := range members {
		fmt.Fprintf(&sb, "[[member]]\nkey = %q\npath = %q\nexclude = false\n\n", m.key, m.path)
	}
	writeFile(t, filepath.Join(realmRoot, ".atomic", "code.toml"), sb.String())
}

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

// Callers must leave opts.Port at 0 and opts.Stdout nil: this helper owns stdout
// so it can parse the startup URL line. shutdown() reaps the goroutine.
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

	if strings.HasSuffix(baseURL, ":0") {
		t.Errorf("server printed port 0 URL: %q", baseURL)
	}

	resp, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz at %s: %v", baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

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

// Deep links resolve client-side, so an unmatched non-API GET must reach the SPA
// shell rather than 404. Only /api/* enforces its own 404s.
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

// Every case builds a real temp-dir layout injected into the resolver, so none
// of this reads the ambient $HOME.
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
				// Resolve only stats the path, so the contents are irrelevant.
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

func TestOpenFlagNonFatalOnError(t *testing.T) {
	dir := t.TempDir()

	var stderr strings.Builder
	baseURL, shutdown := startTestServer(t, serve.Options{
		Open:         true,
		TargetDir:    dir,
		ClaudeMDPath: filepath.Join(t.TempDir(), "CLAUDE.md"),
		Stderr:       &stderr,
		// Swappable seam: always errors.
		BrowserOpener: func(url string) error {
			return fmt.Errorf("fake: open failed")
		},
	})

	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Still serving → the opener error was non-fatal.
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

// Context cancellation is what signal.NotifyContext bridges SIGINT to in
// production, so cancelling stands in for the real shutdown path.
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

	shutdown()

	_, err := http.Get(baseURL + "/healthz")
	if err == nil {
		t.Error("expected connection refused after shutdown, got nil error")
	}
}

// Assets copied into frontend/dist at build time must be served as real static
// files, not swallowed by the SPA index.html fallback.
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

func TestOpenFlagCalledWithURLOnSuccess(t *testing.T) {
	dir := t.TempDir()

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
				// Called twice: drop it rather than block.
			}
			return nil
		},
	})
	defer shutdown()

	// RunWithContext calls the opener synchronously before Serve, and the URL line
	// is printed first, so the channel is already loaded by the time we get here.
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
