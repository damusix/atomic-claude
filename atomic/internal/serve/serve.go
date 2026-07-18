// Package serve implements the `atomic serve` HTTP server — a read-only,
// localhost-only presentation layer over wiki + code-intel data.
//
// The server is a JSON API (/api/*) plus a handful of carried, unreshaped
// endpoints (/graph/data, /code/graph/data, /code/graph/members, /events,
// /healthz) backing the embedded React SPA (frontend_dist.go). Every other
// GET falls through to the SPA shell (index.html); React Router resolves the
// requested path client-side.
package serve

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

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
	_ = scope // scope badge is now surfaced via /api/status, not a server-rendered shell

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
	// store is the single shared realm-snapshot store (CP2 live-reload): nav,
	// page, rail, and graph-data all read through it instead of each
	// tracking its own copy, so the ticker (CP3), lazy per-request
	// validation, and NewSnapshotStore's synchronous warm all observe — and
	// refresh — the same realm state (SC7).
	store := NewSnapshotStore(navRoot)

	// eventsRegistry tracks connected /events (SSE) subscribers. The ticker
	// started below reads its count to decide whether to do any work at all
	// (SC12); NewEventsHandler registers/unregisters through it per connection.
	eventsRegistry := newSubscriberRegistry()

	navOpts := NavOptions{
		RealmRoot:     navRoot,
		IsRealmScope:  isRealmScope,
		WikiIndexPath: wikiIndexPath,
		Store:         store,
	}

	mux := http.NewServeMux()

	// /healthz — liveness probe.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// /api/* — JSON endpoints for the React frontend. Every handler reuses the
	// same view-model builders the pre-cutover htmx fragments used, so link
	// resolution stays single-sourced.
	// Landing relpath: realm scope resolves to the realm index, repo scope to
	// README.md — the /api/page/ handler serves it for an empty relpath so the
	// SPA's "/" route never has to guess the scope.
	landingRel := "README.md"
	if isRealmScope && wikiIndexPath != "" {
		if rel, relErr := filepath.Rel(opts.TargetDir, wikiIndexPath); relErr == nil {
			landingRel = rel
		}
	}
	mux.Handle("/api/page/", NewAPIPageHandler(opts.TargetDir, store, landingRel))
	mux.Handle("/api/file/", NewAPIFileHandler(opts.TargetDir))
	mux.Handle("/api/rail/", NewAPIRailHandler(navRoot, store))
	mux.Handle("/api/nav", NewAPINavHandler(navOpts))
	mux.Handle("/api/search/md", NewAPIMdSearchHandler(MdSearchOptions{NavRoot: navRoot}))
	mux.Handle("/api/code/search", NewAPICodeSearchHandler(CodeSearchOptions{
		RealmRoot:    opts.TargetDir,
		ClaudeMDPath: opts.ClaudeMDPath,
	}))
	mux.Handle("/api/search/stream", NewAPISearchStreamHandler(SearchStreamOptions{
		NavRoot:      navRoot,
		RealmRoot:    opts.TargetDir,
		ClaudeMDPath: opts.ClaudeMDPath,
	}))

	healthOpts := HealthOptions{
		RealmRoot:    navRoot,
		IsRealmScope: isRealmScope,
		// Seams are nil → production defaults wired inside NewAPIStatusHandler.
	}
	mux.Handle("/api/status", NewAPIStatusHandler(healthOpts))
	mux.Handle("/api/external", NewAPIExternalHandler(navRoot, GitOrMtimeDateFn))

	// /events — live-reload SSE stream: register, resync push, stream
	// until the request context ends. Carried path (unchanged by the cutover).
	mux.Handle("/events", NewEventsHandler(store, eventsRegistry))

	// /graph/data — Cytoscape elements JSON for the docs graph view. Shares the
	// store above so /graph/data does not rebuild per-request and stays live.
	mux.Handle("/graph/data", NewGraphDataHandlerWithGraph(navRoot, store))

	// /api/code/* — JSON siblings of the (now-removed) /code/* explorer routes.
	apiExplorerHandler := NewCodeExplorerAPIHandler(CodeExplorerOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		// EngineProvider nil → DefaultEngineProvider.
	})
	for _, route := range []string{
		"/api/code/node",
		"/api/code/callers",
		"/api/code/callees",
		"/api/code/impact",
		"/api/code/files",
		"/api/code/schema",
		"/api/code/file",
	} {
		mux.Handle(route, apiExplorerHandler)
	}

	// /code/graph/data — full-repo code graph export for the code graph view.
	mux.Handle("/code/graph/data", NewCodeGraphHandler(CodeGraphOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		// EngineProvider nil → DefaultEngineProvider.
	}))

	// /code/graph/members — realm member list + indexed state for the code
	// view's member picker.
	mux.Handle("/code/graph/members", NewCodeGraphMembersHandler(CodeGraphOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
	}))

	// Everything else — the SPA shell. React Router resolves /page/<relpath>,
	// /graph, /search, /status, /external, and any deep link client-side.
	// Static assets carried into frontend/dist (app.css, graph-core.js,
	// system-graph.js, code-graph.js, vendor/*, logo.png, the bundled
	// assets/*) are served as-is; any other path falls back to index.html.
	distFS, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: frontend dist sub-fs: %v\n", err)
		return 1
	}
	mux.Handle("/", newSPAHandler(distFS))

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

	srv := &http.Server{
		Handler: mux,
		// BaseContext ties every request's context to the same ctx that
		// drives graceful shutdown below. srv.Shutdown does not itself cancel
		// in-flight request contexts — without this, an open /events
		// connection's ctx.Done() would never fire, and Shutdown would block
		// on it for the full 5s grace window instead of returning promptly.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// The live-reload ticker is bound to the same ctx: it must stop
	// exactly when the server starts shutting down, never before or after.
	startTicker(ctx, store, eventsRegistry, store.tickInterval)

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

// newSPAHandler serves static files from root when the request path matches
// one on disk (app.css, graph-core.js, the bundled assets/* directory, ...),
// and falls back to index.html — the React shell — for everything else
// (deep links like /page/<relpath>, /graph, /search: React Router resolves
// them client-side). Path-traversal is guarded by http.FileServer/http.FS.
func newSPAHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := root.Open(p); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// No file on disk at this path: fall back to the SPA shell.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
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
