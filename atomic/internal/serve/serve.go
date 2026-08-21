// Package serve implements `atomic serve`: a read-only, localhost-only
// presentation layer over wiki and code-intel data. It exposes a JSON API
// (/api/*) plus /graph/data, /code/graph/*, /events, and /healthz, backing the
// embedded React SPA. Every other GET falls through to index.html and is
// resolved client-side.
package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// DisplayScopeRepo is a single repo, with or without a code index.
	DisplayScopeRepo DisplayScope = iota
	// DisplayScopeRealm is the root of a registered wiki realm.
	DisplayScopeRealm
	// DisplayScopeMember is inside exactly one realm member.
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

// ResolveDisplayScope maps realm.Resolve output to a DisplayScope. An
// unindexed repo still resolves — the server must not require a code index
// to start, since a docs-only repo is servable.
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
		return DisplayScopeRepo, nil
	}
}

// Options configures the server. Exported so tests can bypass flag parsing.
type Options struct {
	// Port 0 means OS-assigned.
	Port int
	// Host defaults to loopback; "0.0.0.0" exposes the viewer on the LAN.
	Host string
	// Open triggers a best-effort browser launch after startup.
	Open bool
	// TargetDir defaults to cwd.
	TargetDir string
	// ClaudeMDPath drives realm resolution.
	ClaudeMDPath string
	// Home locates the bus daemon state for /api/bus/*.
	Home string
	// Stdout / Stderr receive log output.
	Stdout io.Writer
	Stderr io.Writer
	// BrowserOpener is the --open seam; nil uses the OS command.
	BrowserOpener func(url string) error
}

// Run parses args, traps SIGINT/SIGTERM, and returns a process exit code.
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

// RunWithContext starts the server and blocks until ctx is cancelled or the
// server fails. Returns 0 on clean shutdown, 1 on error.
func RunWithContext(ctx context.Context, opts Options) int {
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}

	scope, err := ResolveDisplayScope(opts.TargetDir, opts.ClaudeMDPath)
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: scope resolve: %v\n", err)
		return 1
	}
	_ = scope // the scope badge is served from /api/status

	// Second call for the full Resolution, which carries RealmRoot. Cheap —
	// it only reads config files. The error was already surfaced above.
	realmRes, _ := realm.Resolve(opts.TargetDir, opts.ClaudeMDPath)
	isRealmScope := scope == DisplayScopeRealm || scope == DisplayScopeMember
	navRoot := opts.TargetDir
	wikiIndexPath := ""
	if isRealmScope && realmRes.RealmRoot != "" {
		navRoot = realmRes.RealmRoot
		wikiIndexPath = filepath.Join(realmRes.RealmRoot, "wiki", "index.md")
	}
	// One shared snapshot store: nav, page, rail, and graph-data must observe
	// and refresh the same realm state, or live-reload shows them disagreeing.
	store := NewSnapshotStore(navRoot)

	// The ticker reads this count to skip all work when nobody is listening.
	eventsRegistry := newSubscriberRegistry()

	navOpts := NavOptions{
		RealmRoot:     navRoot,
		IsRealmScope:  isRealmScope,
		WikiIndexPath: wikiIndexPath,
		Store:         store,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	// /api/page/ serves this for an empty relpath, so the SPA's "/" route never
	// has to guess the scope.
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
	// The staleness walk takes seconds on a real realm and /api/nav is the
	// shell's first request, so warm it here. If it has not finished by the
	// time the request lands, the request computes it itself.
	if isRealmScope && navOpts.StalenessFn == nil {
		go navStalenessCache.get(navOpts.RealmRoot)
	}
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
		// Nil seams take NewAPIStatusHandler's production defaults.
	}
	mux.Handle("/api/status", NewAPIStatusHandler(healthOpts))
	mux.Handle("/api/external", NewAPIExternalHandler(navRoot, GitOrMtimeDateFn, store))

	plansRegistry := newPlansRegistry()
	plansOpts := plansOptions{
		Root:          navRoot,
		ScopeRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		Registry:      plansRegistry,
	}
	mux.Handle("/api/plans", plansHandler(plansOpts))
	mux.Handle("/api/plans/page", plansPageHandler(plansRegistry))
	mux.Handle("/api/plans/members", plansMembersHandler(plansOpts))

	// Web chat over the atomic bus daemon. See api_bus.go for why this narrows
	// the read-only contract.
	busHome := opts.Home
	if busHome == "" {
		busHome, _ = os.UserHomeDir()
	}
	if busHome != "" {
		mux.Handle("/api/bus/", NewAPIBusHandler(BusAPIOptions{Home: busHome, TargetDir: opts.TargetDir}))
	}

	mux.Handle("/events", NewEventsHandler(store, eventsRegistry))

	// Sharing the store keeps the docs graph off a per-request rebuild.
	mux.Handle("/graph/data", NewGraphDataHandlerWithGraph(navRoot, store))

	apiExplorerHandler := NewCodeExplorerAPIHandler(CodeExplorerOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		// Nil EngineProvider takes DefaultEngineProvider.
	})
	// Loopback-only write surface; see NewAPIReindexHandler for why it exists.
	mux.Handle("/api/code/index", NewAPIReindexHandler(CodeExplorerOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
	}))

	for _, route := range []string{
		"/api/code/node",
		"/api/code/callers",
		"/api/code/callees",
		"/api/code/impact",
		"/api/code/files",
		"/api/code/schema",
		"/api/code/capabilities",
		"/api/code/file",
	} {
		mux.Handle(route, apiExplorerHandler)
	}

	mux.Handle("/code/graph/data", NewCodeGraphHandler(CodeGraphOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
		// Nil EngineProvider takes DefaultEngineProvider.
	}))

	mux.Handle("/code/graph/members", NewCodeGraphMembersHandler(CodeGraphOptions{
		RealmRoot:     opts.TargetDir,
		ClaudeMDPath:  opts.ClaudeMDPath,
		WikiIndexPath: wikiIndexPath,
	}))

	distFS, err := fs.Sub(embeddedFrontend, "frontend/dist")
	if err != nil {
		fmt.Fprintf(opts.Stderr, "atomic serve: frontend dist sub-fs: %v\n", err)
		return 1
	}
	mux.Handle("/", newSPAHandler(distFS))

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

	// A wildcard bind is not a usable URL, so print loopback first and list the
	// reachable LAN addresses under it.
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

	if opts.Open {
		opener := opts.BrowserOpener
		if opener == nil {
			opener = defaultBrowserOpen
		}
		if openErr := opener(url); openErr != nil {
			// Non-fatal — the URL is already on stdout.
			fmt.Fprintf(opts.Stderr, "atomic serve: open browser: %v\n", openErr)
		}
	}

	srv := &http.Server{
		Handler: mux,
		// Shutdown does not cancel in-flight request contexts. Without this,
		// an open /events connection never sees ctx.Done() and Shutdown blocks
		// for the full grace window.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	// Same ctx as the server: the ticker must stop exactly when shutdown starts.
	startTicker(ctx, store, eventsRegistry, store.tickInterval)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			fmt.Fprintf(opts.Stderr, "atomic serve: shutdown: %v\n", err)
			return 1
		}
		return 0
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(opts.Stderr, "atomic serve: %v\n", err)
			return 1
		}
		return 0
	}
}

// newSPAHandler serves an embedded file when the request path names one, and
// index.html otherwise so deep links resolve client-side. Path traversal is
// guarded by http.FileServer.
func newSPAHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	etags := buildAssetETags(root)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if f, err := root.Open(p); err == nil {
			_ = f.Close()
			// Bundle filenames carry no content hash and go:embed zeroes
			// modtimes, so without an explicit validator the browser caches
			// heuristically and an upgrade pairs a new main.js with a stale
			// main.css — which looks like a broken app, not a stale cache.
			if tag, ok := etags[p]; ok {
				w.Header().Set("ETag", tag)
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		if tag, ok := etags["index.html"]; ok {
			w.Header().Set("ETag", tag)
			w.Header().Set("Cache-Control", "no-cache")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

// buildAssetETags hashes every embedded file once at startup. The set cannot
// change while the process runs, so this is never per-request work.
func buildAssetETags(root fs.FS) map[string]string {
	tags := map[string]string{}
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, readErr := fs.ReadFile(root, p)
		if readErr != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		tags[p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	return tags
}

// lanIPv4s returns the host's non-loopback IPv4 addresses, so a wildcard bind
// can print URLs reachable from other devices.
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

// defaultBrowserOpen opens url in the system browser, best-effort.
func defaultBrowserOpen(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// parseFlags builds Options from the serve verb's args.
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
	// Absolute, so every downstream handler resolves request paths against the
	// root no matter how the user spelled the argument.
	var absErr error
	targetDir, absErr = filepath.Abs(targetDir)
	if absErr != nil {
		fmt.Fprintf(stderr, "atomic serve: resolve target dir: %v\n", absErr)
		return Options{}, absErr
	}

	// ClaudeMDPath below mirrors main.go's runCode derivation.
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
		Home:         home,
		Stdout:       stdout,
		Stderr:       stderr,
	}, nil
}
