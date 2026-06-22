// Package-level daemon lifecycle (master CP23).
//
// The daemon binds a per-project unix-domain socket, runs the CP22 go-sdk
// server over each accepted connection, and manages a connection registry
// with clock-injectable reaper and auto-shutdown.
//
// Design: docs/design/code-intel-engine.md §MCP server lifecycle.
// Spec:   docs/spec/code-intel-surfaces.md §MCP server lifecycle (contract).
package mcp

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
)

// ---------------------------------------------------------------------------
// Named constants (axiom 2 / R6 — asserted by TestDaemonConstants)
// ---------------------------------------------------------------------------

// Exported so tests can assert their literal values (spec R6 guard).
const (
	ConnIdleTTL   = 30 * time.Minute
	ServerIdleTTL = 30 * time.Minute
	ReapTick      = 60 * time.Second
	// SyncInterval is the default interval at which the daemon re-syncs the
	// served symbol graph against the working tree. 0 disables the poller.
	SyncInterval = 10 * time.Second
)

// Keep unexported aliases for internal use so existing code compiles unchanged.
const (
	connIdleTTL   = ConnIdleTTL
	serverIdleTTL = ServerIdleTTL
	reapTick      = ReapTick
)

// ---------------------------------------------------------------------------
// Socket and lock path helpers
// ---------------------------------------------------------------------------

// SocketPathFromDB returns the unix-socket path derived from the db file path.
// The socket lives in the same directory as the db — not inside the source tree.
//
// For a standalone repo (dbPath = <repo>/.claude/.atomic-index/atomic.db), the
// socket is at <repo>/.claude/.atomic-index/mcp.sock — the same location as
// before the fix. For a realm member (dbPath = <realm>/.atomic/<key>.db), the
// socket is at <realm>/.atomic/<key>.mcp.sock — inside the realm's .atomic dir,
// not inside the member's source tree.
func SocketPathFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	stem := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	return filepath.Join(dir, stem+".mcp.sock")
}

// LockPathFromDB returns the flock lock path derived from the db file path.
// Mirrors SocketPathFromDB: same directory, same stem, .mcp.lock extension.
func LockPathFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	stem := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	return filepath.Join(dir, stem+".mcp.lock")
}

// SocketPath returns the unix-socket path for the given project root.
// Deprecated: use SocketPathFromDB for new code. Retained for tests that
// use a standalone project root where db lives at the canonical position.
func SocketPath(projectRoot string) string {
	return SocketPathFromDB(filepath.Join(projectRoot, ".claude", ".atomic-index", "atomic.db"))
}

// LockPath returns the flock lock path for the given project root.
// Deprecated: use LockPathFromDB for new code. Retained for tests that
// use a standalone project root where db lives at the canonical position.
func LockPath(projectRoot string) string {
	return LockPathFromDB(filepath.Join(projectRoot, ".claude", ".atomic-index", "atomic.db"))
}

// ---------------------------------------------------------------------------
// Connection registry
// ---------------------------------------------------------------------------

// connEntry tracks one accepted connection.
type connEntry struct {
	createdAt time.Time
	updatedAt time.Time
}

// registry is a clock-injectable connection registry used by the daemon.
// All methods are safe for concurrent use.
type registry struct {
	mu      sync.Mutex
	entries map[string]*connEntry
	now     func() time.Time // injectable for tests
}

// NewRegistry creates an exported registry. Exposed for tests.
func NewRegistry(now func() time.Time) *registry {
	return newRegistry(now)
}

func newRegistry(now func() time.Time) *registry {
	return &registry{
		entries: make(map[string]*connEntry),
		now:     now,
	}
}

// Add registers a new connection. Exported for tests.
func (r *registry) Add(id string) { r.add(id) }

// add registers a new connection.
func (r *registry) add(id string) {
	t := r.now()
	r.mu.Lock()
	r.entries[id] = &connEntry{createdAt: t, updatedAt: t}
	r.mu.Unlock()
}

// Touch refreshes updatedAt for the given connection id. Exported for tests.
func (r *registry) Touch(id string) { r.touch(id) }

// touch refreshes updatedAt for the given connection id.
func (r *registry) touch(id string) {
	t := r.now()
	r.mu.Lock()
	if e, ok := r.entries[id]; ok {
		e.updatedAt = t
	}
	r.mu.Unlock()
}

// Remove deletes a connection entry. Exported for tests.
func (r *registry) Remove(id string) { r.remove(id) }

// remove deletes a connection entry.
func (r *registry) remove(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

// Count returns the number of tracked connections. Exported for tests.
func (r *registry) Count() int { return r.count() }

// count returns the number of tracked connections.
func (r *registry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

// Idle returns the ids of connections whose updatedAt is older than ttl. Exported for tests.
func (r *registry) Idle(ttl time.Duration) []string { return r.idle(ttl) }

// idle returns the ids of connections whose updatedAt is older than ttl.
func (r *registry) idle(ttl time.Duration) []string {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for id, e := range r.entries {
		if now.Sub(e.updatedAt) > ttl {
			out = append(out, id)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Daemon
// ---------------------------------------------------------------------------

// SyncFunc is the function the sync poller calls on each tick.
// Injected as a field so tests can substitute a spy without needing a real engine.
// In production RunDaemon wires it to eng.Sync.
type SyncFunc func(ctx context.Context) error

// Daemon is the per-project unix-socket MCP singleton daemon.
//
// Use RunDaemon to start it from the `atomic code __serve` internal verb.
// Use NewTestDaemon to construct one with injectable durations for tests.
type Daemon struct {
	socketPath string
	listener   net.Listener
	srv        *sdk.Server
	reg        *registry

	// injectable clock — real time in production, fake in tests
	now func() time.Time

	// injectable durations — real constants in production, short values in tests
	idleD time.Duration
	reapD time.Duration
	// syncD is the background sync interval; 0 disables the poller.
	syncD time.Duration
	// syncFn is called by the sync poller on each tick.
	// In production it is eng.Sync; in tests it may be a spy.
	syncFn SyncFunc

	// connClosers maps connID → close function; used by reaper to force-close
	mu          sync.Mutex
	connClosers map[string]func()

	// done is closed when the daemon finishes
	done chan struct{}
}

// RegistryCount returns the number of currently registered connections.
// Exported as a test seam so tests can assert warm-reuse and shutdown conditions
// without polling IsLive (which creates a connection and may interfere with
// idle-shutdown tests).
func (d *Daemon) RegistryCount() int {
	return d.reg.count()
}

// RunDaemon opens or inits the engine at sourceRoot with the index at dbPath,
// creates the MCP server, binds the socket (next to dbPath), and runs the
// accept loop until auto-shutdown. The socket file is removed on clean exit.
//
// Using explicit (sourceRoot, dbPath) makes the daemon cwd-independent: it
// never consults the process working directory or the realm resolver, so it
// can be spawned from a realm root (or any non-git directory) without exiting
// before binding its socket.
//
// now is injectable for tests; pass nil for real time.
// watchInterval controls the background sync poller; 0 disables it.
func RunDaemon(ctx context.Context, sourceRoot, dbPath string, now func() time.Time, watchInterval time.Duration) error {
	if now == nil {
		now = time.Now
	}

	eng, err := engine.NewWithDBPath(sourceRoot, dbPath)
	if err != nil {
		return fmt.Errorf("daemon: create engine: %w", err)
	}
	defer eng.Close()

	if eng.IsInitialized() {
		if err := eng.Open(ctx); err != nil {
			return fmt.Errorf("daemon: open engine: %w", err)
		}
	} else {
		if err := eng.Init(ctx); err != nil {
			return fmt.Errorf("daemon: init engine: %w", err)
		}
	}

	stats, err := eng.GetStats(ctx)
	fileCount := 0
	if err == nil {
		fileCount = stats.FileCount
	}

	srv := NewServer(eng, fileCount)
	sockPath := SocketPathFromDB(dbPath)

	// Ensure the db directory exists before writing the socket (the engine's
	// Init already creates it via MkdirAll; this is a safety net for callers
	// that pass a pre-existing db without going through Init first).
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return fmt.Errorf("daemon: mkdir socket dir: %w", err)
	}

	// Remove stale socket if present.
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon: listen %s: %w", sockPath, err)
	}

	d := &Daemon{
		socketPath:  sockPath,
		listener:    ln,
		srv:         srv,
		reg:         newRegistry(now),
		now:         now,
		idleD:       serverIdleTTL,
		reapD:       reapTick,
		syncD:       watchInterval,
		syncFn:      eng.Sync,
		connClosers: make(map[string]func()),
		done:        make(chan struct{}),
	}

	return d.Run(ctx)
}

// NewTestDaemon constructs a Daemon with injectable idle, reap, and sync
// durations for tests. It creates the unix listener on sockPath itself so the
// socket is live as soon as this call returns — callers can safely poll IsLive
// immediately after NewTestDaemon without waiting for a goroutine to start.
//
// idleDuration controls how long the daemon waits with no connections before
// shutting down. reapDuration controls the reaper tick interval.
// syncDuration controls the background sync interval; 0 disables the poller.
// syncFn is called by the poller on each tick; pass nil to use a no-op.
// Pass nil for now to use real time.
func NewTestDaemon(sockPath string, srv *sdk.Server, now func() time.Time, idleDuration, reapDuration, syncDuration time.Duration, syncFn SyncFunc) *Daemon {
	if now == nil {
		now = time.Now
	}
	if syncFn == nil {
		syncFn = func(_ context.Context) error { return nil }
	}
	// Remove stale socket if present (mirrors RunDaemon behaviour).
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		// This is a test helper; panic is appropriate for setup failures.
		panic("NewTestDaemon: listen " + sockPath + ": " + err.Error())
	}
	return &Daemon{
		socketPath:  sockPath,
		listener:    ln,
		srv:         srv,
		reg:         newRegistry(now),
		now:         now,
		idleD:       idleDuration,
		reapD:       reapDuration,
		syncD:       syncDuration,
		syncFn:      syncFn,
		connClosers: make(map[string]func()),
		done:        make(chan struct{}),
	}
}

// RunAcceptLoop runs a minimal accept loop over the provided listener.
// It wraps each accepted connection in an IOTransport and calls srv.Connect.
// Used by the e2e test to drive a real unix-socket session without the full
// daemon lifecycle (no reaper, no idle shutdown).
//
// The loop exits when the listener is closed or ctx is done.
func RunAcceptLoop(ctx context.Context, ln net.Listener, srv *sdk.Server, sockPath string) error {
	defer func() {
		_ = ln.Close()
		_ = os.Remove(sockPath)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return err
			}
		}
		go func(c net.Conn) {
			defer c.Close()
			transport := &sdk.IOTransport{
				Reader: c,
				Writer: c,
			}
			ss, err := srv.Connect(ctx, transport, nil)
			if err != nil {
				return
			}
			ss.Wait()
		}(conn)
	}
}

// Run is the main accept + lifecycle loop. Exported so NewTestDaemon users can call it.
func (d *Daemon) Run(ctx context.Context) error {
	// Start reaper goroutine.
	reaperDone := make(chan struct{})
	go d.reapLoop(ctx, reaperDone)

	// Start sync poller goroutine if enabled (syncD > 0).
	syncDone := make(chan struct{})
	if d.syncD > 0 {
		go d.syncLoop(ctx, syncDone)
	} else {
		close(syncDone)
	}

	// Cleanup in reverse order: first close done (unblocks reaper + sync poller),
	// then wait for both goroutines to exit, then clean up listener and socket file.
	// Defers execute LIFO so declare them in reverse execution order.
	defer func() {
		_ = d.listener.Close()
		_ = os.Remove(d.socketPath)
	}()
	defer func() { <-syncDone }()
	defer func() { <-reaperDone }()
	defer close(d.done)

	// Accept loop.
	connCh := make(chan net.Conn)
	acceptErr := make(chan error, 1)
	go func() {
		for {
			c, err := d.listener.Accept()
			if err != nil {
				acceptErr <- err
				return
			}
			connCh <- c
		}
	}()

	idleTimer := (*time.Timer)(nil) // nil until registry empties
	idleFired := make(chan struct{}, 1)

	// armIdleTimer arms the idle shutdown timer. Called ONLY when the registry
	// drops to zero (from handleConn's onEmpty callback). Must not be called when
	// connections are still active.
	armIdleTimer := func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		// Use a single-fire goroutine so the timer fires into a channel we own.
		idleTimer = time.AfterFunc(d.idleDuration(), func() {
			select {
			case idleFired <- struct{}{}:
			default:
			}
		})
	}

	// Start idle timer immediately (0 conns at startup).
	armIdleTimer()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-acceptErr:
			// Listener closed — expected on shutdown.
			return err

		case conn := <-connCh:
			// New connection: cancel any pending idle shutdown.
			if idleTimer != nil {
				idleTimer.Stop()
				idleTimer = nil
			}
			// Drain idleFired if it fired concurrently.
			select {
			case <-idleFired:
			default:
			}
			d.handleConn(ctx, conn, armIdleTimer)

		case <-idleFired:
			// Re-check: a connection may have arrived during the idle window.
			// If so, the armIdleTimer was already stopped by the connCh case,
			// but the fired event may have already been queued. Do not close
			// the listener with live connections.
			if d.reg.count() > 0 {
				continue
			}
			// Registry is empty and TTL has elapsed — shut down.
			_ = d.listener.Close()
			return nil
		}
	}
}

// idleDuration returns the server idle TTL. Uses injectable field so tests can shorten it.
func (d *Daemon) idleDuration() time.Duration {
	if d.idleD > 0 {
		return d.idleD
	}
	return serverIdleTTL
}

// handleConn accepts one connection and runs it in a goroutine.
// onEmpty is called when the registry reaches zero — it arms the idle timer.
// It is NOT called on every connection exit; only when Count()==0 after remove.
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn, onEmpty func()) {
	connID := conn.RemoteAddr().String() + fmt.Sprintf("@%d", d.now().UnixNano())
	d.reg.add(connID)

	// Wrap net.Conn so every Read touches the registry (per-request updatedAt).
	tc := &touchingConn{Conn: conn, reg: d.reg, connID: connID}

	// IOTransport wraps the byte-level reader/writer; net.Conn satisfies both
	// io.ReadCloser and io.WriteCloser (Read/Write/Close on net.Conn).
	// We pass the touchingConn as both reader and writer — it embeds net.Conn
	// so its Write and Close delegate transparently.
	transport := &sdk.IOTransport{
		Reader: tc,
		Writer: tc,
	}

	// closeOnce ensures the connection is closed exactly once from either the
	// reaper or the session's natural end.
	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}

	d.mu.Lock()
	d.connClosers[connID] = closeConn
	d.mu.Unlock()

	go func() {
		defer func() {
			closeConn()
			d.reg.remove(connID)
			d.mu.Lock()
			delete(d.connClosers, connID)
			d.mu.Unlock()
			// Arm the idle timer only when the registry is now empty.
			// A connection arriving during the idle window will cancel the timer
			// (handled in Run's connCh case), so we arm here unconditionally when
			// empty — the re-check in the idleFired case handles any late arrivals.
			if d.reg.count() == 0 {
				onEmpty()
			}
		}()

		ss, err := d.srv.Connect(ctx, transport, nil)
		if err != nil {
			// context.Canceled is the normal close path (daemon shutting down);
			// other errors indicate an unexpected transport or protocol failure.
			if ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "daemon: srv.Connect %s: %v\n", connID, err)
			}
			return
		}

		// Wait for the session to finish.
		ss.Wait()
	}()
}

// reapLoop ticks at reapTick intervals and force-closes idle connections.
func (d *Daemon) reapLoop(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(d.reapInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.done:
			return
		case <-ticker.C:
			d.reapOnce()
		}
	}
}

// reapInterval returns the reap tick duration. Uses injectable field so tests can shorten it.
func (d *Daemon) reapInterval() time.Duration {
	if d.reapD > 0 {
		return d.reapD
	}
	return reapTick
}

// syncLoop ticks at syncD intervals and calls syncFn. It is single-flight:
// it never starts a new sync while one is already in progress.
// Stops when ctx is cancelled or d.done is closed.
func (d *Daemon) syncLoop(ctx context.Context, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(d.syncD)
	defer ticker.Stop()

	var inFlight sync.Mutex

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.done:
			return
		case <-ticker.C:
			// Single-flight: skip if a sync is already running.
			if !inFlight.TryLock() {
				continue
			}
			go func() {
				defer inFlight.Unlock()
				_ = d.syncFn(ctx)
			}()
		}
	}
}

// reapOnce force-closes all connections idle longer than connIdleTTL.
func (d *Daemon) reapOnce() {
	idle := d.reg.idle(connIdleTTL)
	for _, id := range idle {
		d.mu.Lock()
		closer, ok := d.connClosers[id]
		d.mu.Unlock()
		if ok {
			closer()
		}
	}
}

// ---------------------------------------------------------------------------
// touchingConn — wraps net.Conn, updates registry on every Read
// ---------------------------------------------------------------------------

// touchingConn wraps a net.Conn and calls reg.touch(connID) on every
// successful Read, recording last-activity time per the spec contract.
// The touch fires at the bytes layer so it covers every incoming MCP
// request without needing to intercept the SDK's jsonrpc.Message layer.
type touchingConn struct {
	net.Conn
	reg    *registry
	connID string
}

func (c *touchingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err == nil && n > 0 {
		c.reg.touch(c.connID)
	}
	return n, err
}

// ---------------------------------------------------------------------------
// Liveness check
// ---------------------------------------------------------------------------

// IsLive reports whether the daemon is running by attempting a connection to
// the socket. Returns false on absence or ECONNREFUSED.
func IsLive(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
