// Package serve implements the `atomic serve` HTTP server — a read-only,
// localhost-only presentation layer over wiki + code-intel data.
//
// CP1 delivers the server skeleton: flag parsing, scope resolution, embedded
// assets, three-pane HTML shell, /healthz, graceful SIGINT shutdown, and the
// --open browser seam.
package serve

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

//go:embed assets templates
var embeddedFS embed.FS

// systemGraphFragmentHTML is the htmx fragment for the /graph (Network View) page.
// The [data-system-graph] container is the seam the shell's onLoad handler keys on
// to mount Cytoscape; the loading line is removed once the layout settles.
const systemGraphFragmentHTML = `<div id="system-cy" data-system-graph></div>
<p class="loading system-graph-loading">Laying out graph…</p>`

// DisplayScope is the serve-level scope label derived from the realm resolver.
type DisplayScope int

const (
	// DisplayScopeRepo: a single repo (with or without a code index).
	DisplayScopeRepo DisplayScope = iota
	// DisplayScopeRealm: cwd is the root of a registered wiki realm.
	DisplayScopeRealm
	// DisplayScopeMember: cwd is inside exactly one realm member.
	DisplayScopeMember
)

func (d DisplayScope) String() string {
	switch d {
	case DisplayScopeRepo:
		return "Repo"
	case DisplayScopeRealm:
		return "Realm"
	case DisplayScopeMember:
		return "Member"
	default:
		return "Unknown"
	}
}

// ResolveDisplayScope maps realm.Resolve output to a DisplayScope.
// ScopeNoIndex → DisplayScopeRepo: a bare repo with no index is still
// servable (docs-only); the server must not require a code index to start.
func ResolveDisplayScope(cwd, claudeMDPath string) (DisplayScope, error) {
	res, err := realm.Resolve(cwd, claudeMDPath)
	if err != nil {
		return DisplayScopeRepo, err
	}
	switch res.Scope {
	case realm.ScopeRealmAll:
		return DisplayScopeRealm, nil
	case realm.ScopeRealmMember:
		return DisplayScopeMember, nil
	default:
		// ScopeRepo (local index) and ScopeNoIndex (bare repo) both map to
		// DisplayScopeRepo — docs + code when indexed, docs-only otherwise.
		return DisplayScopeRepo, nil
	}
}

// Options holds all configuration for the server.  Exported so tests can
// construct it directly without going through flag parsing.
type Options struct {
	// Port is the TCP port to bind. 0 = OS-assigned.
	Port int
	// Host is the bind address. Empty defaults to 127.0.0.1 (loopback only).
	// Set to "0.0.0.0" to expose the (read-only) viewer on all interfaces / the LAN.
	Host string
	// Open triggers a best-effort browser launch after startup.
	Open bool
	// TargetDir is the directory being served (positional arg, default cwd).
	TargetDir string
	// ClaudeMDPath is the CLAUDE.md path used for realm resolution.
	ClaudeMDPath string
	// Stdout / Stderr receive log output.
	Stdout io.Writer
	Stderr io.Writer
	// BrowserOpener is the seam for --open. nil → default OS command.
	// Tests inject a stub to verify error-non-fatality without spawning a browser.
	BrowserOpener func(url string) error
}

// Run is the os.Exit-aware entry point called by main.go.
// It wires signal.NotifyContext for SIGINT/SIGTERM, then delegates to RunWithContext.
func Run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stdout, stderr)
	if err != nil {
		return 2
	}
	opts.Stdout = stdout
	opts.Stderr = stderr

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return RunWithContext(ctx, opts)
}

// RunWithContext starts the HTTP server and blocks until ctx is cancelled or
// the server fails.  Returns 0 on clean shutdown, 1 on error.
// This function is the testable entry point — tests inject a context and Options.
func RunWithContext(ctx context.Context, opts Options) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	// Resolve display scope.
	scope, err := ResolveDisplayScope(opts.TargetDir, opts.ClaudeMDPath)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: scope resolve: %v\n", err)
		return 1
	}

	// Resolve realm root for the nav tree.  We call realm.Resolve a second time
	// here to get the full Resolution (which carries RealmRoot).  The double-call
	// is cheap — it only reads config files.  A future refactor may unify these.
	realmRes, _ := realm.Resolve(opts.TargetDir, opts.ClaudeMDPath) // error already surfaced above
	isRealmScope := scope == DisplayScopeRealm || scope == DisplayScopeMember
	navRoot := opts.TargetDir
	wikiIndexPath := ""
	if isRealmScope && realmRes.RealmRoot != "" {
		navRoot = realmRes.RealmRoot
		wikiIndexPath = filepath.Join(realmRes.RealmRoot, "wiki", "index.md")
	}
	navOpts := NavOptions{
		RealmRoot:     navRoot,
		IsRealmScope:  isRealmScope,
		WikiIndexPath: wikiIndexPath,
	}

	// Parse and cache the embedded template.
	tmplData, err := embeddedFS.ReadFile("templates/layout.html")
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: read layout template: %v\n", err)
		return 1
	}
	tmpl, err := template.New("layout").Parse(string(tmplData))
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: parse layout template: %v\n", err)
		return 1
	}

	// Build the static file server over the embedded assets subtree.
	assetsFS, err := fs.Sub(embeddedFS, "assets")
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: assets sub-fs: %v\n", err)
		return 1
	}
	staticHandler := http.FileServer(http.FS(assetsFS))

	mux := http.NewServeMux()

	// /healthz — liveness probe.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// /static/* — embedded assets, served from memory.
	mux.Handle("/static/", http.StripPrefix("/static/", staticHandler))

	// Compute the default landing URL server-side.
	// Realm scope → /page/wiki/index.md; repo/member scope → /page/README.md
	// (the /page/ handler falls back gracefully for missing files).
	landingURL := computeLandingURL(opts.TargetDir, isRealmScope, wikiIndexPath)

	// ShellRenderer is shared by all routes that need to render the full shell
	// (/, /page/*, /file/* on document loads). FE8: shell is the universal envelope.
	shell := &ShellRenderer{
		tmpl:       tmpl,
		ScopeBadge: scope.String(),
		ScopeLabel: opts.TargetDir,
		TargetDir:  opts.TargetDir,
	}

	// Build the link graph once at server start. BuildLinkGraph is read-only and
	// cheap at wiki-scale; rebuilding per-request (as /graph/data does) is also
	// acceptable but a single build here keeps the rail handler stateless.
	linkGraph := BuildLinkGraph(navRoot)

	// /page/* — render a markdown file from the scope root, wired to the rail.
	// FE8: non-htmx requests receive the shell with LandingURL = the requested path.
	mux.Handle("/page/", NewPageHandlerWithGraph(opts.TargetDir, linkGraph, shell))

	// /rail/* — right-rail compositing: three OOB fragments for the focused page
	// (#rail-out-content, #rail-in-content, #rail-graph-content).
	mux.Handle("/rail/", NewRailHandler(navRoot, linkGraph))

	// /file/* — syntax-highlighted source view from the scope root.
	// FE8: non-htmx requests receive the shell with LandingURL = the requested path.
	mux.Handle("/file/", NewFileHandler(opts.TargetDir, shell))

	// /external — external-link registry page.
	mux.Handle("/external", NewExternalHandler(navRoot, GitOrMtimeDateFn))

	// /nav — nav tree fragment (htmx target: #nav-pane).
	mux.Handle("/nav", NewNavHandler(navOpts))

	// /status — realm-health dashboard (FE-SC6: health is ambient, not the landing).
	// Was /health in CP6; demoted to /status so the landing is the page view.
	healthOpts := HealthOptions{
		RealmRoot:    navRoot,
		IsRealmScope: isRealmScope,
		// Seams are nil → production defaults wired inside NewHealthHandler.
	}
	mux.Handle("/status", NewHealthHandler(healthOpts))

	// /search — dedicated full-pane search page (streams results via SSE).
	// Document loads are shell-wrapped; the search dialog links here for "view all".
	mux.Handle("/search", NewSearchPageHandler(shell))

	// /search/stream — Server-Sent Events: md block + one code event per member
	// (members searched concurrently), terminal "end". Backs the /search page.
	mux.Handle("/search/stream", NewSearchStreamHandler(SearchStreamOptions{
		NavRoot:      navRoot,
		RealmRoot:    opts.TargetDir,
		ClaudeMDPath: opts.ClaudeMDPath,
	}))

	// /search/md — markdown full-text search fragment (dialog quick-jump).
	mux.Handle("/search/md", NewMdSearchHandler(MdSearchOptions{NavRoot: navRoot}))

	// /code/search — federated code search (CP7, SC9).
	mux.Handle("/code/search", NewCodeSearchHandler(CodeSearchOptions{
		RealmRoot:    opts.TargetDir,
		ClaudeMDPath: opts.ClaudeMDPath,
		// SearchFn nil → production default (DefaultMemberSearchFn).
	}))

	// /graph/data — Cytoscape elements JSON (CP9, SC11).
	// FE8: pass the startup-built linkGraph so /graph/data does not rebuild per-request.
	mux.Handle("/graph/data", NewGraphDataHandlerWithGraph(navRoot, linkGraph))

	// /graph — the Network View as its own page (URL-addressable, history-tracked,
	// refresh-survivable). htmx requests get the #system-cy mount fragment; the
	// shell's onLoad handler mounts Cytoscape and restores zoom/pan from the URL
	// (?z=&px=&py=). Document loads (refresh / share / Back) get the full shell with
	// LandingURL=/graph so it boots straight into the graph.
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		if fragmentRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, systemGraphFragmentHTML)
			return
		}
		if err := shell.Render(w, "/graph", http.StatusOK); err != nil {
			fmt.Fprintf(opts.Stderr, "atomic serve: graph shell render: %v\n", err)
		}
	})

	// /code/* — per-repo Code Explorer (CP8, SC10).
	// Routes: /code/node, /code/callers, /code/callees, /code/impact, /code/files, /code/schema.
	explorerHandler := NewCodeExplorerHandler(CodeExplorerOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		// EngineProvider nil → DefaultEngineProvider.
	})
	for _, route := range []string{
		"/code/node",
		"/code/callers",
		"/code/callees",
		"/code/impact",
		"/code/files",
		"/code/schema",
		"/code/file",
	} {
		mux.Handle(route, explorerHandler)
	}

	// / — Obsidian shell (FE1: breadcrumb + search + [page|system] toggle + right rail).
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if err := shell.Render(w, landingURL, http.StatusOK); err != nil {
			// Headers already sent — log only.
			fmt.Fprintf(opts.Stderr, "atomic serve: template execute: %v\n", err)
		}
	})

	// Bind listener. Default to loopback; an explicit Host (e.g. 0.0.0.0) opts into
	// exposing the read-only viewer on other interfaces / the LAN.
	bindHost := opts.Host
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bindHost, opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: listen %s: %v\n", addr, err)
		return 1
	}

	// Resolve actual address (important when Port == 0). A wildcard bind isn't a
	// usable URL, so the primary line shows loopback (works locally + for --open);
	// reachable LAN addresses are listed below it for other devices.
	actualAddr := ln.Addr().(*net.TCPAddr)
	wildcard := bindHost == "0.0.0.0" || bindHost == "::"
	displayHost := bindHost
	if wildcard {
		displayHost = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%d", displayHost, actualAddr.Port)
	fmt.Fprintln(opts.Stdout, url)
	if wildcard {
		for _, ip := range lanIPv4s() {
			fmt.Fprintf(opts.Stdout, "http://%s:%d\n", ip, actualAddr.Port)
		}
	}

	// Best-effort browser open.
	if opts.Open {
		opener := opts.BrowserOpener
		if opener == nil {
			opener = defaultBrowserOpen
		}
		if openErr := opener(url); openErr != nil {
			// Non-fatal: log to stderr and continue.
			fmt.Fprintf(opts.Stderr, "atomic serve: open browser: %v\n", openErr)
		}
	}

	// Page routes content-negotiate on the HX-Request header (htmx fragment vs
	// full shell), so every response varies by it. Without Vary, a shared or
	// browser cache could serve a bare fragment to a direct navigation, or a full
	// document to an htmx swap. Set it once for all routes; it is harmless on the
	// routes that do not negotiate.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Vary", "HX-Request")
		mux.ServeHTTP(w, r)
	})

	srv := &http.Server{Handler: handler}

	// Serve in a background goroutine.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	// Wait for context cancellation (SIGINT/SIGTERM in production, cancel() in tests).
	select {
	case <-ctx.Done():
		// Graceful shutdown.
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			fmt.Fprintf(opts.Stderr, "atomic serve: shutdown: %v\n", err)
			return 1
		}
		return 0
	case err := <-serveErr:
		// http.ErrServerClosed is normal — Shutdown was called concurrently.
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(opts.Stderr, "atomic serve: %v\n", err)
			return 1
		}
		return 0
	}
}

// lanIPv4s returns the non-loopback IPv4 addresses of the host's interfaces, so a
// wildcard (0.0.0.0) bind can print URLs reachable from other devices on the LAN.
func lanIPv4s() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			out = append(out, ip4.String())
		}
	}
	return out
}

// defaultBrowserOpen opens url in the system browser. Best-effort only.
func defaultBrowserOpen(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		// Linux and everything else: try xdg-open.
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// ShellRenderer holds the shell template and the fixed scope metadata that
// every page rendered inside the shell requires. Handlers use it to emit the
// full layout.html shell with a custom LandingURL for document (non-htmx) loads.
type ShellRenderer struct {
	tmpl       *template.Template
	ScopeBadge string
	ScopeLabel string
	TargetDir  string
}

// Render writes layout.html to w with LandingURL set to initialContentURL.
// status is the HTTP status code (200 or 404). The Content-Type header is set
// before Execute so callers need not set it themselves.
func (s *ShellRenderer) Render(w http.ResponseWriter, initialContentURL string, status int) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	data := struct {
		ScopeBadge string
		ScopeLabel string
		TargetDir  string
		LandingURL string
	}{
		ScopeBadge: s.ScopeBadge,
		ScopeLabel: s.ScopeLabel,
		TargetDir:  s.TargetDir,
		LandingURL: initialContentURL,
	}
	return s.tmpl.Execute(w, data)
}

// computeLandingURL returns the /page/ URL that #main-pane loads on startup.
//
// Decision: realm scope → realm index (wiki/index.md); repo/member → README.md.
// The /page/ handler already handles a missing file gracefully (404 fragment),
// so no additional file-existence check is needed here.
func computeLandingURL(targetDir string, isRealmScope bool, wikiIndexPath string) string {
	if isRealmScope && wikiIndexPath != "" {
		// wikiIndexPath is an absolute path; derive the repo-relative path using
		// filepath.Rel so a trailing slash on targetDir never causes an off-by-one.
		// Example: /realm/root/wiki/index.md relative to /realm/root → wiki/index.md
		rel, err := filepath.Rel(targetDir, wikiIndexPath)
		if err == nil {
			return "/page/" + rel
		}
	}
	// Repo / member scope: try README.md.
	return "/page/README.md"
}

// parseFlags parses the args slice for the serve verb and returns Options.
// stdout/stderr are used only for the flag.FlagSet usage output.
func parseFlags(args []string, stdout, stderr io.Writer) (Options, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var port int
	var open bool
	var host string
	fs.IntVar(&port, "port", 4500, "TCP port to listen on (0 = OS-assigned free port)")
	fs.BoolVar(&open, "open", false, "open the browser after startup (best-effort)")
	fs.StringVar(&host, "host", "127.0.0.1", "bind address (use 0.0.0.0 to expose the read-only viewer on the LAN)")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}

	// Optional positional arg: target directory.
	targetDir := ""
	if fs.NArg() > 0 {
		targetDir = fs.Arg(0)
	}
	if targetDir == "" {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "atomic serve: get cwd: %v\n", err)
			return Options{}, err
		}
	}
	// Normalize to an absolute path so downstream handlers (page, rail, file,
	// link graph) can resolve request paths against the root regardless of how
	// the user invoked "atomic serve" (e.g. "atomic serve ." or a relative path).
	var absErr error
	targetDir, absErr = filepath.Abs(targetDir)
	if absErr != nil {
		fmt.Fprintf(stderr, "atomic serve: resolve target dir: %v\n", absErr)
		return Options{}, absErr
	}

	// claudeMDPath mirrors main.go:runCode derivation.
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "atomic serve: get home: %v\n", err)
		return Options{}, err
	}

	return Options{
		Port:         port,
		Host:         host,
		Open:         open,
		TargetDir:    targetDir,
		ClaudeMDPath: fmt.Sprintf("%s/.claude/CLAUDE.md", home),
		Stdout:       stdout,
		Stderr:       stderr,
	}, nil
}
