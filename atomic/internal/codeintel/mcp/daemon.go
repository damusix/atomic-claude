// Daemon lifecycle: a per-project unix socket serving MCP over each accepted
// connection, with an idle reaper and auto-shutdown. Contract in
// docs/spec/code-intel-surfaces.md.
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
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// Exported so tests can assert the literal values.
const (
	ConnIdleTTL   = 30 * time.Minute
	ServerIdleTTL = 30 * time.Minute
	ReapTick      = 60 * time.Second
	// SyncInterval re-syncs the served graph against the working tree; 0 disables.
	SyncInterval = 10 * time.Second
)

const (
	connIdleTTL   = ConnIdleTTL
	serverIdleTTL = ServerIdleTTL
	reapTick      = ReapTick
)

// SocketPathFromDB places the socket beside the db rather than in the source
// tree, so a realm member's socket lands in the realm's .atomic directory
// instead of inside the checked-out repo.
func SocketPathFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	stem := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	return filepath.Join(dir, stem+".mcp.sock")
}

// LockPathFromDB mirrors SocketPathFromDB with a .mcp.lock extension.
func LockPathFromDB(dbPath string) string {
	dir := filepath.Dir(dbPath)
	stem := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	return filepath.Join(dir, stem+".mcp.lock")
}

// SocketPath assumes the db sits at the canonical position under projectRoot.
//
// Deprecated: use SocketPathFromDB, which works for realm members too.
func SocketPath(projectRoot string) string {
	return SocketPathFromDB(config.IndexDBPath(projectRoot))
}

// LockPath assumes the db sits at the canonical position under projectRoot.
//
// Deprecated: use LockPathFromDB, which works for realm members too.
func LockPath(projectRoot string) string {
	return LockPathFromDB(config.IndexDBPath(projectRoot))
}

type connEntry struct {
	createdAt time.Time
	updatedAt time.Time
}

// registry tracks live connections. Safe for concurrent use; the clock is a
// field so tests can drive idle expiry without sleeping.
type registry struct {
	mu      sync.Mutex
	entries map[string]*connEntry
	now     func() time.Time
}

// The exported registry methods below exist only so tests in other packages can
// drive it; production code uses the unexported forms.

func NewRegistry(now func() time.Time) *registry {
	return newRegistry(now)
}

func newRegistry(now func() time.Time) *registry {
	return &registry{
		entries: make(map[string]*connEntry),
		now:     now,
	}
}

func (r *registry) Add(id string) { r.add(id) }

func (r *registry) add(id string) {
	t := r.now()
	r.mu.Lock()
	r.entries[id] = &connEntry{createdAt: t, updatedAt: t}
	r.mu.Unlock()
}

func (r *registry) Touch(id string) { r.touch(id) }

func (r *registry) touch(id string) {
	t := r.now()
	r.mu.Lock()
	if e, ok := r.entries[id]; ok {
		e.updatedAt = t
	}
	r.mu.Unlock()
}

func (r *registry) Remove(id string) { r.remove(id) }

func (r *registry) remove(id string) {
	r.mu.Lock()
	delete(r.entries, id)
	r.mu.Unlock()
}

func (r *registry) Count() int { return r.count() }

func (r *registry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *registry) Idle(ttl time.Duration) []string { return r.idle(ttl) }

// idle returns connections untouched for longer than ttl.
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

// SyncFunc is a field rather than a call to eng.Sync so tests can spy on it
// without standing up a real engine. RunDaemon wires it to eng.Sync.
type SyncFunc func(ctx context.Context) error

// Daemon is the per-project unix-socket MCP singleton. Start it with RunDaemon;
// NewTestDaemon builds one with short, injectable durations.
type Daemon struct {
	socketPath string
	listener   net.Listener
	srv        *sdk.Server
	reg        *registry

	// Clock and durations are fields so tests need neither real time nor sleeps.
	now   func() time.Time
	idleD time.Duration
	reapD time.Duration
	// syncD of 0 disables the poller.
	syncD  time.Duration
	syncFn SyncFunc

	// connID → force-close, used by the reaper.
	mu          sync.Mutex
	connClosers map[string]func()

	done chan struct{}
}

// RegistryCount is a test seam: polling IsLive instead would open a connection
// and so perturb the very idle-shutdown behaviour under test.
func (d *Daemon) RegistryCount() int {
	return d.reg.count()
}

// RunDaemon binds the socket next to dbPath and serves until auto-shutdown.
//
// Taking sourceRoot and dbPath explicitly is what makes the daemon
// cwd-independent: it consults neither the working directory nor the realm
// resolver, so it survives being spawned from a realm root or any non-git
// directory. Pass nil for now to use real time; watchInterval 0 disables sync.
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

	// Init normally creates this; the MkdirAll covers callers that hand us a
	// pre-existing db and so never went through Init.
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return fmt.Errorf("daemon: mkdir socket dir: %w", err)
	}

	// A socket left by an unclean exit would make Listen fail.
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

// NewTestDaemon binds the listener before returning, so a caller can poll
// IsLive immediately instead of racing a goroutine that has yet to start.
// Nil now means real time; nil syncFn means a no-op.
func NewTestDaemon(sockPath string, srv *sdk.Server, now func() time.Time, idleDuration, reapDuration, syncDuration time.Duration, syncFn SyncFunc) *Daemon {
	if now == nil {
		now = time.Now
	}
	if syncFn == nil {
		syncFn = func(_ context.Context) error { return nil }
	}
	_ = os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
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

// RunAcceptLoop serves connections with no reaper and no idle shutdown, so an
// e2e test can drive a real socket session without the daemon lifecycle.
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

// Run is the accept and lifecycle loop.
func (d *Daemon) Run(ctx context.Context) error {
	reaperDone := make(chan struct{})
	go d.reapLoop(ctx, reaperDone)

	syncDone := make(chan struct{})
	if d.syncD > 0 {
		go d.syncLoop(ctx, syncDone)
	} else {
		close(syncDone)
	}

	// Shutdown must run close(done) → wait for both goroutines → drop the
	// listener and socket. Defers are LIFO, so they are declared in reverse.
	defer func() {
		_ = d.listener.Close()
		_ = os.Remove(d.socketPath)
	}()
	defer func() { <-syncDone }()
	defer func() { <-reaperDone }()
	defer close(d.done)

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

	idleTimer := (*time.Timer)(nil)
	idleFired := make(chan struct{}, 1)

	// Only ever called with an empty registry — arming it while connections are
	// live would schedule a shutdown out from under them.
	armIdleTimer := func() {
		if idleTimer != nil {
			idleTimer.Stop()
		}
		idleTimer = time.AfterFunc(d.idleDuration(), func() {
			select {
			case idleFired <- struct{}{}:
			default:
			}
		})
	}

	armIdleTimer()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-acceptErr:
			return err

		case conn := <-connCh:
			if idleTimer != nil {
				idleTimer.Stop()
				idleTimer = nil
			}
			// Stop() does not unqueue an event the timer already sent.
			select {
			case <-idleFired:
			default:
			}
			d.handleConn(ctx, conn, armIdleTimer)

		case <-idleFired:
			// The event may predate a connection that arrived mid-window, so
			// never close the listener on a stale fire.
			if d.reg.count() > 0 {
				continue
			}
			_ = d.listener.Close()
			return nil
		}
	}
}

func (d *Daemon) idleDuration() time.Duration {
	if d.idleD > 0 {
		return d.idleD
	}
	return serverIdleTTL
}

// handleConn serves one connection in a goroutine. onEmpty fires only when the
// registry reaches zero, not on every connection exit.
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn, onEmpty func()) {
	connID := conn.RemoteAddr().String() + fmt.Sprintf("@%d", d.now().UnixNano())
	d.reg.add(connID)

	tc := &touchingConn{Conn: conn, reg: d.reg, connID: connID}

	transport := &sdk.IOTransport{
		Reader: tc,
		Writer: tc,
	}

	// The reaper and the session's natural end both close; only one may win.
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
			if d.reg.count() == 0 {
				onEmpty()
			}
		}()

		ss, err := d.srv.Connect(ctx, transport, nil)
		if err != nil {
			// A cancelled context is the ordinary shutdown path, not a failure.
			if ctx.Err() == nil {
				fmt.Fprintf(os.Stderr, "daemon: srv.Connect %s: %v\n", connID, err)
			}
			return
		}

		ss.Wait()
	}()
}

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

func (d *Daemon) reapInterval() time.Duration {
	if d.reapD > 0 {
		return d.reapD
	}
	return reapTick
}

// syncLoop is single-flight, and waits for the in-flight sync before returning
// so no sync ever runs against an engine that is already closing.
func (d *Daemon) syncLoop(ctx context.Context, done chan struct{}) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(done)
	}()

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
			if !inFlight.TryLock() {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer inFlight.Unlock()
				// Cancellation may have raced the tick.
				if ctx.Err() != nil {
					return
				}
				_ = d.syncFn(ctx)
			}()
		}
	}
}

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

// touchingConn records last-activity at the bytes layer, which covers every
// incoming request without reaching into the SDK's jsonrpc.Message layer.
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

// IsLive dials the socket; a leftover socket file with no daemon behind it
// gives ECONNREFUSED and so reads as not live.
func IsLive(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
