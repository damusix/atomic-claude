package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
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

// A served root whose state root refuses stops serve at startup, before any
// request handler could reach the path helpers' fatal backstop.
func TestRunWithContext_RefusingServedRootExitsAtStartup(t *testing.T) {
	root := t.TempDir()
	record := config.StateLocationRecordPath(root)
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(record) })

	var stderr strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := serve.RunWithContext(ctx, serve.Options{TargetDir: root, Stdout: io.Discard, Stderr: &stderr})
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "selection") {
		t.Errorf("stderr = %q, want the refusal", stderr.String())
	}
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
				dbPath := config.IndexDBPath(dir)
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

func TestRunWithContext_LinkedRealmContent(t *testing.T) {
	const (
		wikiIndex = "wiki/index.md"
		concern   = "wiki/concerns/outline.md"
		bucket    = "buckets/research"
		note      = bucket + "/sessions/note.md"
		draft     = "buckets/drafts/draft.md"
		quiet     = bucket + "/sessions/quiet.md"
		loose     = "welcome.md"
		query     = "orchardtoken"
		private   = "AGENTS.local.md"
		stateNote = ".atomic/private-note.md"

		member        = "repos/notebook"
		memberState   = "member-state"
		memberDoc     = member + "/docs/field-notes.md"
		memberPrivate = member + "/" + memberState + "/internal.md"
		memberQuery   = "notebooktoken"
		memberVisible = memberQuery + " field notes"

		otherMember  = "repos/almanac"
		otherState   = "repos/almanac-state"
		otherDoc     = otherMember + "/docs/field-notes.md"
		otherPrivate = otherState + "/internal.md"

		relocatedMember  = "repos/seasonal"
		relocatedState   = "archive-state"
		relocatedDoc     = relocatedMember + "/docs/field-notes.md"
		relocatedPrivate = relocatedState + "/internal.md"
	)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("Git unavailable: %v", err)
	}
	workspace := t.TempDir()
	home := filepath.Join(workspace, "home")
	publisher := filepath.Join(workspace, "publisher")
	consumer := filepath.Join(workspace, "consumer")
	remote := filepath.Join(workspace, "team.git")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.StateDirEnvVar, "")
	t.Cleanup(config.SetHomeDirForTest(home))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_TEMPLATE_DIR", "")
	t.Setenv("GIT_AUTHOR_NAME", "Realm fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "realm@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Realm fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "realm@example.test")

	link := func(target, path string) {
		t.Helper()
		if err := os.Symlink(target, path); err != nil {
			if runtime.GOOS == "windows" {
				t.Skipf("native symlinks unavailable: %v", err)
			}
			t.Fatalf("symlink %s: %v", path, err)
		}
	}
	writeFile(t, filepath.Join(home, "fixture.txt"), "Fixture\n")
	link("fixture.txt", filepath.Join(home, "fixture-link"))

	if out, err := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--bare", "--initial-branch=main", remote).CombinedOutput(); err != nil {
		t.Fatalf("initialize fixture remote: %v\n%s", err, out)
	}
	writeFile(t, filepath.Join(publisher, wikiIndex), `# Team wiki

<wiki-buckets>
<bucket name="research" path="buckets/research"/>
<bucket name="drafts" path="buckets/drafts"/>
</wiki-buckets>

[Outline](concerns/outline)
`)
	snippet := func(label string) string { return query + " " + label }
	sharedDocs := map[string]string{
		concern: "outline",
		note:    "observation",
		loose:   "welcome",
	}
	for path, label := range sharedDocs {
		writeFile(t, filepath.Join(publisher, path), "# "+label+"\n\n"+snippet(label)+"\n")
	}
	writeFile(t, filepath.Join(publisher, draft), "# Draft\n\n"+snippet("draft")+"\n")
	share, err := realmshare.PreviewShare(t.Context(), publisher, realmshare.ShareOptions{
		Remote: remote,
		Paths:  []string{"wiki", bucket, "buckets/drafts", loose},
	})
	if err != nil {
		t.Fatalf("preview fixture publication: %v", err)
	}
	if err := realmshare.ApplyShare(t.Context(), share); err != nil {
		t.Fatalf("publish fixture: %v", err)
	}

	for _, path := range []string{private, stateNote} {
		abs := filepath.Join(consumer, path)
		writeFile(t, abs, "# Personal notes\n\n"+snippet("personal")+"\n")
		backdateFile(t, abs)
	}
	joined, err := realmshare.Join(t.Context(), consumer, remote)
	if err != nil {
		t.Fatalf("join fixture realm: %v", err)
	}
	review, err := realmshare.PreviewLink(t.Context(), consumer, []string{"wiki", bucket, loose})
	if err != nil {
		t.Fatalf("preview selected fixture links: %v", err)
	}
	if err := realmshare.ApplyLink(t.Context(), review); err != nil {
		t.Fatalf("install selected fixture links: %v", err)
	}
	if _, err := os.Stat(filepath.Join(joined.Binding.ClonePath, draft)); err != nil {
		t.Fatalf("check unlinked draft is present in the shared checkout: %v", err)
	}
	buckets, err := realmshare.WikiBuckets(consumer)
	if err != nil {
		t.Fatalf("check fixture bucket registration: %v", err)
	}
	if !buckets.SharedWiki || len(buckets.Shared) != 1 || buckets.Shared[0].Name != "research" {
		t.Fatalf("fixture registrations = %+v, want owned wiki and research only", buckets)
	}

	bucketRoot, err := filepath.EvalSymlinks(filepath.Join(consumer, bucket))
	if err != nil {
		t.Fatalf("resolve fixture bucket: %v", err)
	}
	link("sessions", filepath.Join(bucketRoot, "archive"))
	link(filepath.Join("sessions", "note.md"), filepath.Join(bucketRoot, "copy.md"))
	for path := range sharedDocs {
		backdateFile(t, filepath.Join(consumer, path))
	}
	backdateFile(t, filepath.Join(consumer, wikiIndex))
	quietPath := filepath.Join(consumer, quiet)
	writeFile(t, quietPath, "# Quiet notes\n\nA recent field observation.\n")
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(quietPath, future, future); err != nil {
		t.Fatalf("set quiet fixture mtime: %v", err)
	}

	for _, fixture := range []struct{ repo, state, doc, private string }{
		{member, memberState, memberDoc, memberPrivate},
		{otherMember, filepath.Join(consumer, otherState), otherDoc, otherPrivate},
		{relocatedMember, memberState, relocatedDoc, relocatedPrivate},
	} {
		memberRoot := filepath.Join(consumer, fixture.repo)
		memberDocPath := filepath.Join(consumer, fixture.doc)
		memberPrivatePath := filepath.Join(consumer, fixture.private)
		writeFile(t, memberDocPath, "# Field notes\n\n"+memberVisible+"\n")
		if out, err := exec.CommandContext(t.Context(), "git", "-C", memberRoot, "init", "--quiet").CombinedOutput(); err != nil {
			t.Fatalf("initialize fixture member %q: %v\n%s", fixture.repo, err, out)
		}
		if _, err := config.PersistStateSelection(memberRoot, fixture.state); err != nil {
			t.Fatalf("select fixture member %q state: %v", fixture.repo, err)
		}
		writeFile(t, memberPrivatePath, "# Internal notes\n\n"+memberQuery+" internal notes\n")
		backdateFile(t, memberDocPath)
		backdateFile(t, memberPrivatePath)
	}
	link(filepath.Join("..", "..", relocatedState), filepath.Join(consumer, relocatedMember, memberState))

	claudeMD := filepath.Join(home, "CLAUDE.md")
	buildClaudeMD(t, claudeMD, []string{filepath.Join(consumer, wikiIndex)})
	baseURL, shutdown := startTestServer(t, serve.Options{
		TargetDir: consumer, ClaudeMDPath: claudeMD, Home: home,
	})
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}
	get := func(t *testing.T, route string, status int) (string, http.Header) {
		t.Helper()
		resp, err := client.Get(baseURL + route)
		if err != nil {
			t.Fatalf("GET %s: %v", route, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read %s: %v", route, err)
		}
		if resp.StatusCode != status {
			t.Fatalf("GET %s = %d, want %d: %s", route, resp.StatusCode, status, body)
		}
		return string(body), resp.Header
	}
	decode := func(t *testing.T, body string, dst any) {
		t.Helper()
		if err := json.Unmarshal([]byte(body), dst); err != nil {
			t.Fatalf("decode response: %v\n%s", err, body)
		}
	}

	omittedDocs := []string{
		relocatedPrivate, draft, bucket + "/archive/note.md", bucket + "/copy.md",
		private, stateNote, memberPrivate, otherPrivate,
	}
	t.Run("unlinked and private paths", func(t *testing.T) {
		for _, path := range omittedDocs {
			for _, route := range []string{"/api/page/", "/api/file/"} {
				t.Run(route+path, func(t *testing.T) {
					get(t, route+path, http.StatusNotFound)
				})
			}
		}
	})

	t.Run("navigation", func(t *testing.T) {
		body, _ := get(t, "/api/nav", http.StatusOK)
		var nav navResponseWire
		decode(t, body, &nav)
		if nav.Scope != "realm" {
			t.Errorf("nav scope = %q, want realm", nav.Scope)
		}
		for _, want := range []struct{ group, path string }{
			{"Realm", wikiIndex}, {"Concerns", concern}, {"Buckets", note},
		} {
			var hasPath func([]navNodeWire) bool
			hasPath = func(items []navNodeWire) bool {
				return slices.ContainsFunc(items, func(item navNodeWire) bool {
					return item.RelPath == want.path || hasPath(item.Children)
				})
			}
			if !slices.ContainsFunc(nav.Groups, func(group navGroupWire) bool {
				return group.Name == want.group && hasPath(group.Items)
			}) {
				t.Errorf("nav group %q missing %q: %s", want.group, want.path, body)
			}
		}
		for _, omitted := range []string{"drafts", "archive", "copy.md", ".atomic", private} {
			if strings.Contains(body, omitted) {
				t.Errorf("nav includes %q: %s", omitted, body)
			}
		}
	})

	t.Run("content", func(t *testing.T) {
		t.Run("landing", func(t *testing.T) {
			body, _ := get(t, "/api/page/", http.StatusOK)
			var page struct{ HTML, RelPath string }
			decode(t, body, &page)
			if page.RelPath != wikiIndex || !strings.Contains(page.HTML, "Team wiki") {
				t.Errorf("landing page = %+v, want %q and Team wiki", page, wikiIndex)
			}
			if want := `href="/page/` + concern + `"`; !strings.Contains(page.HTML, want) {
				t.Errorf("landing page link missing %q: %s", want, page.HTML)
			}

			body, _ = get(t, "/graph/data", http.StatusOK)
			var graph cytoscapeElements
			decode(t, body, &graph)
			for _, edge := range graph.Edges {
				if edge.Data.Source == wikiIndex && edge.Data.Target == concern {
					return
				}
			}
			t.Errorf("graph missing landing link %q → %q: %+v", wikiIndex, concern, graph.Edges)
		})
		for path, label := range sharedDocs {
			t.Run(path, func(t *testing.T) {
				body, _ := get(t, "/api/page/"+path, http.StatusOK)
				var page struct {
					HTML       string
					RelPath    string
					Breadcrumb []struct{ Label, Path string }
				}
				decode(t, body, &page)
				if page.RelPath != path || !strings.Contains(page.HTML, snippet(label)) {
					t.Errorf("page = %+v, want %q and %q", page, path, snippet(label))
				}
				parent, filename := filepath.ToSlash(filepath.Dir(path)), filepath.Base(path)
				if n := len(page.Breadcrumb); n == 0 || page.Breadcrumb[n-1].Label != filename ||
					(parent != "." && (n < 2 || page.Breadcrumb[n-2].Path != parent)) {
					t.Errorf("breadcrumb for %q = %+v", path, page.Breadcrumb)
				}

				body, _ = get(t, "/api/file/"+path, http.StatusOK)
				var source struct{ HTML, Path string }
				decode(t, body, &source)
				if source.Path != path || !strings.Contains(source.HTML, snippet(label)) {
					t.Errorf("source = %+v, want %q and %q", source, path, snippet(label))
				}
			})
		}
	})

	t.Run("markdown search", func(t *testing.T) {
		assertMatches := func(body string) {
			var got struct {
				Query   string
				Results []struct {
					RelPath string
					Line    int
					Snippet string
				}
			}
			decode(t, body, &got)
			if got.Query != query || len(got.Results) != len(sharedDocs) {
				t.Errorf("search = %+v, want %q matches only in %v", got, query, sharedDocs)
			}
			seen := make(map[string]bool)
			for _, hit := range got.Results {
				if label, ok := sharedDocs[hit.RelPath]; !ok || seen[hit.RelPath] || hit.Line != 3 || hit.Snippet != snippet(label) {
					t.Errorf("unexpected search hit: %+v", hit)
				}
				seen[hit.RelPath] = true
			}
		}
		body, _ := get(t, "/api/search/md?q="+query, http.StatusOK)
		assertMatches(body)
		body, hdr := get(t, "/api/search/stream?src=md&q="+query, http.StatusOK)
		if got := hdr.Get("Content-Type"); got != "text/event-stream" {
			t.Errorf("stream Content-Type = %q, want text/event-stream", got)
		}
		events := parseSSE(t, body)
		if len(events) != 2 || events[0].Name != "md" || events[1] != (sseEvent{Name: "end", Data: "{}"}) {
			t.Fatalf("search stream = %+v, want md then end", events)
		}
		assertMatches(events[0].Data)
	})

	t.Run("nested member document", func(t *testing.T) {
		for _, path := range []string{memberDoc, otherDoc, relocatedDoc} {
			t.Run(path, func(t *testing.T) {
				body, _ := get(t, "/api/page/"+path, http.StatusOK)
				var page struct{ HTML, RelPath string }
				decode(t, body, &page)
				if page.RelPath != path || !strings.Contains(page.HTML, memberVisible) {
					t.Errorf("member page = %+v, want %q and %q", page, path, memberVisible)
				}
				body, _ = get(t, "/api/file/"+path, http.StatusOK)
				var source struct{ HTML, Path string }
				decode(t, body, &source)
				if source.Path != path || !strings.Contains(source.HTML, memberVisible) {
					t.Errorf("member source = %+v, want %q and %q", source, path, memberVisible)
				}
			})
		}
		body, _ := get(t, "/api/search/md?q="+memberQuery, http.StatusOK)
		var search struct{ Results []struct{ RelPath string } }
		decode(t, body, &search)
		var paths []string
		for _, result := range search.Results {
			paths = append(paths, result.RelPath)
		}
		slices.Sort(paths)
		if want := []string{otherDoc, memberDoc, relocatedDoc}; !slices.Equal(paths, want) {
			t.Errorf("member search paths = %v, want %v", paths, want)
		}
	})

	readGraph := func(t *testing.T) (map[string]string, string) {
		t.Helper()
		body, hdr := get(t, "/graph/data", http.StatusOK)
		var graph struct {
			Nodes []struct {
				Data struct{ ID, Snippet string }
			}
		}
		decode(t, body, &graph)
		nodes := make(map[string]string, len(graph.Nodes))
		for _, node := range graph.Nodes {
			nodes[node.Data.ID] = node.Data.Snippet
		}
		return nodes, hdr.Get("X-Graph-Fingerprint")
	}

	t.Run("graph refresh", func(t *testing.T) {
		nodes, before := readGraph(t)
		for path, label := range sharedDocs {
			if got := nodes[path]; got != snippet(label) {
				t.Errorf("graph node %q snippet = %q, want %q", path, got, snippet(label))
			}
		}
		for _, path := range []string{memberDoc, otherDoc, relocatedDoc} {
			if got := nodes[path]; got != memberVisible {
				t.Errorf("graph node %q snippet = %q, want %q", path, got, memberVisible)
			}
		}
		for _, path := range omittedDocs {
			if _, ok := nodes[path]; ok {
				t.Errorf("graph includes excluded node %q", path)
			}
		}
		edited := filepath.Join(consumer, note)
		writeFile(t, edited, "# observation\n\n"+snippet("revised observation")+"\n")
		backdateFile(t, edited)

		nodes, after := readGraph(t)
		if before == "" || after == "" || before == after {
			t.Errorf("fingerprint after shared edit = %q, want nonempty and different from %q", after, before)
		}
		if got := nodes[note]; got != snippet("revised observation") {
			t.Errorf("updated graph node %q snippet = %q", note, got)
		}
	})

	t.Run("quiet document deletion", func(t *testing.T) {
		nodes, before := readGraph(t)
		if got := nodes[quiet]; got != "A recent field observation." {
			t.Fatalf("quiet document %q missing from graph before deletion: %q", quiet, got)
		}
		if err := os.Remove(quietPath); err != nil {
			t.Fatalf("delete quiet document: %v", err)
		}

		nodes, after := readGraph(t)
		if _, ok := nodes[quiet]; ok {
			t.Errorf("graph retained deleted quiet document %q", quiet)
		}
		if before == "" || after == "" || before == after {
			t.Errorf("fingerprint after quiet document deletion = %q, want nonempty and different from %q", after, before)
		}
	})

	t.Run("unlink", func(t *testing.T) {
		paths := []string{wikiIndex, concern, note, loose}
		nodes, before := readGraph(t)
		for _, path := range paths {
			if _, ok := nodes[path]; !ok {
				t.Errorf("graph missing linked node %q before unlink", path)
			}
		}
		if err := realmshare.Unlink(t.Context(), consumer); err != nil {
			t.Fatalf("unlink fixture realm: %v", err)
		}

		nodes, after := readGraph(t)
		if before == "" || after == "" || before == after {
			t.Errorf("fingerprint after unlink = %q, want nonempty and different from %q", after, before)
		}
		for _, path := range paths {
			if _, ok := nodes[path]; ok {
				t.Errorf("graph retained unlinked node %q", path)
			}
		}

		body, _ := get(t, "/api/nav", http.StatusOK)
		for _, path := range []string{concern, note} {
			if strings.Contains(body, `"`+path+`"`) {
				t.Errorf("nav retained unlinked document %q: %s", path, body)
			}
		}

		assertNoMatches := func(body string) {
			var got struct{ Results []json.RawMessage }
			decode(t, body, &got)
			if len(got.Results) != 0 {
				t.Errorf("search retained matches after unlink: %s", body)
			}
		}
		body, _ = get(t, "/api/search/md?q="+query, http.StatusOK)
		assertNoMatches(body)
		body, _ = get(t, "/api/search/stream?src=md&q="+query, http.StatusOK)
		events := parseSSE(t, body)
		if len(events) != 2 || events[0].Name != "md" || events[1].Name != "end" {
			t.Errorf("search stream after unlink = %+v, want md then end", events)
		} else {
			assertNoMatches(events[0].Data)
		}

		for _, path := range paths {
			for _, route := range []string{"/api/page/", "/api/file/"} {
				t.Run(route+path, func(t *testing.T) {
					get(t, route+path, http.StatusNotFound)
				})
			}
		}
	})
}
