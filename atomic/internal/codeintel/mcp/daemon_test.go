// Tests for the CP23 daemon lifecycle (master CP23).
//
// Design contract: docs/spec/code-intel-surfaces.md §MCP server lifecycle.
//
// Clock-injectable: the registry/reaper/auto-shutdown are driven by a fake
// clock so all timing assertions are instant, with no real sleep needed.
// Spawn seam: auto-start tests inject an in-process stub that starts a
// goroutine daemon instead of a subprocess.
package mcp_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/engine"
	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

// ---------------------------------------------------------------------------
// TestDaemonConstants — R6: literal values asserted
// ---------------------------------------------------------------------------

// TestDaemonConstants asserts the named lifecycle constants match the spec.
// This is the R6 check: if a constant drifts the test fails immediately.
func TestDaemonConstants(t *testing.T) {
	if codemcp.ConnIdleTTL != 30*time.Minute {
		t.Errorf("ConnIdleTTL = %v, want 30m", codemcp.ConnIdleTTL)
	}
	if codemcp.ServerIdleTTL != 30*time.Minute {
		t.Errorf("ServerIdleTTL = %v, want 30m", codemcp.ServerIdleTTL)
	}
	if codemcp.ReapTick != 60*time.Second {
		t.Errorf("ReapTick = %v, want 60s", codemcp.ReapTick)
	}
	// R6: SyncInterval must be exactly 10s.
	if codemcp.SyncInterval != 10*time.Second {
		t.Errorf("SyncInterval = %v, want 10s", codemcp.SyncInterval)
	}
}

// ---------------------------------------------------------------------------
// TestSyncPoller — background self-sync goroutine
// ---------------------------------------------------------------------------

// TestSyncPoller_SyncCalledOnInterval verifies that after the sync interval
// elapses the daemon calls Sync on its engine.
//
// Why: the daemon is meant to keep the served symbol graph fresh; without this
// test a no-op syncLoop or a disabled poller would silently pass.
func TestSyncPoller_SyncCalledOnInterval(t *testing.T) {
	dir := tmpShortDir(t, "sync")
	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	// Short idle so daemon stays alive even with 0 connections during the test.
	const shortIdle = 5 * time.Second
	const shortSync = 50 * time.Millisecond

	syncCalled := make(chan struct{}, 10)
	syncFn := func(_ context.Context) error {
		select {
		case syncCalled <- struct{}{}:
		default:
		}
		return nil
	}

	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, shortSync, syncFn)
	go func() { _ = d.Run(ctx) }()
	waitForSocket(t, sockPath, 3*time.Second)

	// Wait for at least one Sync call within a reasonable deadline.
	select {
	case <-syncCalled:
		// pass: poller fired and invoked Sync
	case <-time.After(3 * time.Second):
		t.Fatal("syncFn was not called within 3s — poller did not fire")
	}
}

// TestSyncPoller_StopsOnCtxCancel verifies the poller goroutine stops when
// the context is cancelled.
//
// Why: a goroutine that outlives the daemon leaks resources and may call Sync
// on a closed engine.
func TestSyncPoller_StopsOnCtxCancel(t *testing.T) {
	dir := tmpShortDir(t, "synccancel")
	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	const shortIdle = 5 * time.Second
	const shortSync = 50 * time.Millisecond

	// cancelFired is set to 1 just before ctx is cancelled so the spy can
	// distinguish pre-cancel syncs (expected) from post-cancel ones (leak).
	var cancelFired atomic.Int32
	var syncAfterCancel atomic.Bool
	syncFn := func(_ context.Context) error {
		if cancelFired.Load() == 1 {
			syncAfterCancel.Store(true)
		}
		return nil
	}

	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, shortSync, syncFn)
	daemonDone := make(chan struct{})
	go func() {
		defer close(daemonDone)
		_ = d.Run(ctx)
	}()
	waitForSocket(t, sockPath, 3*time.Second)

	// Let at least one sync fire so we know the poller is running.
	time.Sleep(shortSync * 3)

	// Mark cancel-boundary then cancel — poller must not fire after this.
	cancelFired.Store(1)
	cancel()
	select {
	case <-daemonDone:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop after ctx cancel")
	}

	// Wait an extra poller interval to catch any late fires from an escaped goroutine.
	time.Sleep(shortSync * 3)
	if syncAfterCancel.Load() {
		t.Fatal("syncFn was called after ctx cancel — poller goroutine leaked past shutdown")
	}
}

// TestSyncPoller_NoWatch verifies that when syncD==0 (no-watch mode), the
// syncFn is never called.
//
// Why: --no-watch must disable the poller entirely; without this test a
// zero-interval ticker would fire immediately and bypass the guard.
func TestSyncPoller_NoWatch(t *testing.T) {
	dir := tmpShortDir(t, "nowatch")
	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	const shortIdle = 5 * time.Second

	syncCalled := make(chan struct{}, 1)
	syncFn := func(_ context.Context) error {
		select {
		case syncCalled <- struct{}{}:
		default:
		}
		return nil
	}

	// syncD == 0 disables the poller.
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, syncFn)
	go func() { _ = d.Run(ctx) }()
	waitForSocket(t, sockPath, 3*time.Second)

	// Wait 200ms — syncFn should never be called.
	select {
	case <-syncCalled:
		t.Fatal("syncFn was called with syncD==0 — --no-watch mode broken")
	case <-time.After(200 * time.Millisecond):
		// pass: poller is disabled
	}
}

// ---------------------------------------------------------------------------
// TestIsLive — socket liveness
// ---------------------------------------------------------------------------

func TestIsLive_AbsentSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "missing.sock")
	if codemcp.IsLive(sock) {
		t.Fatal("IsLive should be false for absent socket")
	}
}

func TestIsLive_LiveListener(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	// Accept in background so the connect handshake completes.
	go func() {
		c, _ := ln.Accept()
		if c != nil {
			c.Close()
		}
	}()

	if !codemcp.IsLive(sock) {
		t.Fatal("IsLive should be true for a live listener")
	}
}

func TestIsLive_StaleSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "stale.sock")

	// Create a socket file without a listener.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln.Close() // close without accepting — leaves socket file

	// After close the socket should be dead (ECONNREFUSED).
	if codemcp.IsLive(sock) {
		t.Fatal("IsLive should be false for a closed listener (ECONNREFUSED)")
	}
}

// ---------------------------------------------------------------------------
// TestRegistry — connection registry operations
// ---------------------------------------------------------------------------

func TestRegistry_AddTouchRemove(t *testing.T) {
	now := time.Now()
	clock := &fakeClock{t: now}
	reg := codemcp.NewRegistry(clock.Now)

	reg.Add("c1")
	if reg.Count() != 1 {
		t.Fatalf("count after Add: got %d, want 1", reg.Count())
	}

	// Advance clock by 5 min — c1 is NOT idle (idleTTL = 30m).
	clock.Advance(5 * time.Minute)
	reg.Touch("c1")
	idle := reg.Idle(codemcp.ConnIdleTTL)
	if len(idle) != 0 {
		t.Fatalf("expected no idle conns, got %v", idle)
	}

	// Advance past idleTTL — c1 is now idle.
	clock.Advance(31 * time.Minute)
	idle = reg.Idle(codemcp.ConnIdleTTL)
	if len(idle) != 1 || idle[0] != "c1" {
		t.Fatalf("expected [c1] idle, got %v", idle)
	}

	reg.Remove("c1")
	if reg.Count() != 0 {
		t.Fatalf("count after Remove: got %d, want 0", reg.Count())
	}
}

func TestRegistry_Reap_DropsIdleNotFresh(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	reg := codemcp.NewRegistry(clock.Now)

	// Add two connections. Touch c2 (fresh) but not c1 (idle).
	reg.Add("c1")
	reg.Add("c2")
	clock.Advance(15 * time.Minute)
	reg.Touch("c2")

	// Advance 20 more minutes: c1 idle for 35m (>30m), c2 fresh for 20m.
	clock.Advance(20 * time.Minute)

	idle := reg.Idle(codemcp.ConnIdleTTL)
	if len(idle) != 1 || idle[0] != "c1" {
		t.Fatalf("expected only c1 idle, got %v", idle)
	}
}

// ---------------------------------------------------------------------------
// TestAutoStart — flock-guarded single spawn
// ---------------------------------------------------------------------------

// TestAutoStart_SpawnCalledOnce verifies that when the socket is absent,
// exactly one spawn is invoked even under concurrent proxy calls.
func TestAutoStart_SpawnCalledOnce(t *testing.T) {
	dir := tmpShortDir(t, "as")
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")

	// Spawn stub: starts an in-process listener daemon on the socket.
	var spawnCount atomic.Int32
	stub := func(sourceRoot, db string, _ codemcp.WatchOptions) error {
		spawnCount.Add(1)
		return startInProcessDaemonWithDB(t, sourceRoot, db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 5 concurrent proxy callers.
	const goroutines = 5
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = codemcp.EnsureRunning(ctx, dir, dbPath, stub)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: ensureRunning: %v", i, err)
		}
	}

	if n := spawnCount.Load(); n != 1 {
		t.Errorf("spawn called %d times, want exactly 1 (double-spawn guard failed)", n)
	}
}

// TestAutoStart_StaleSocketRemoved verifies that a leftover socket file (whose
// listener is dead) is removed before spawning.
func TestAutoStart_StaleSocketRemoved(t *testing.T) {
	dir := tmpShortDir(t, "stale")
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")

	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a stale socket file: on macOS net.Listen+Close removes the file,
	// so we write a dummy file directly to simulate a crashed daemon's leftover.
	if err := os.WriteFile(sockPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}

	if _, err := os.Stat(sockPath); os.IsNotExist(err) {
		t.Fatal("stale socket file should exist before ensureRunning")
	}

	var spawned atomic.Bool
	stub := func(sourceRoot, db string, _ codemcp.WatchOptions) error {
		spawned.Store(true)
		return startInProcessDaemonWithDB(t, sourceRoot, db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := codemcp.EnsureRunning(ctx, dir, dbPath, stub); err != nil {
		t.Fatalf("ensureRunning: %v", err)
	}
	if !spawned.Load() {
		t.Fatal("spawn should have been called (stale socket should trigger restart)")
	}
}

// ---------------------------------------------------------------------------
// TestWarmReuse — two clients, engine opened once
// ---------------------------------------------------------------------------

// TestWarmReuse verifies that a second connection to a running daemon reuses
// the warm engine (registry shows 2 connections while both are live).
func TestWarmReuse(t *testing.T) {
	dir := tmpShortDir(t, "warm")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	// Use a long idle duration so the daemon stays alive throughout the test.
	const longIdle = 30 * time.Second
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, longIdle, longIdle, 0, nil)
	go func() {
		_ = d.Run(ctx)
	}()
	waitForSocket(t, sockPath, 3*time.Second)

	// Open two connections to the same socket simultaneously.
	c1, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial 1: %v", err)
	}
	defer c1.Close()

	c2, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial 2: %v", err)
	}
	defer c2.Close()

	// Poll until both connections are registered — no sleep.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.RegistryCount() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Assert both connections are registered (proves warm reuse: engine opened once, 2 live conns).
	if got := d.RegistryCount(); got != 2 {
		t.Fatalf("RegistryCount = %d, want 2 (warm reuse: both connections should be registered)", got)
	}

	if !codemcp.IsLive(sockPath) {
		t.Fatal("daemon should still be live with 2 clients connected")
	}
}

// ---------------------------------------------------------------------------
// TestAutoShutdown — clock-injectable
// ---------------------------------------------------------------------------

// TestAutoShutdown verifies that when 0 connections persist for serverIdleTTL,
// the daemon removes the socket file and exits.
func TestAutoShutdown_SocketRemovedAfterIdle(t *testing.T) {
	dir := tmpShortDir(t, "idle")
	sockPath := codemcp.SocketPath(dir)

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build engine and server before starting the goroutine so NewTestDaemon
	// (which calls net.Listen) runs promptly after the goroutine starts, well
	// within the waitForSocket deadline.
	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	// Start the daemon with a very short idle duration so the test doesn't need
	// to wait 30 real minutes. NewTestDaemon calls net.Listen synchronously so
	// the socket is live before the goroutine starts.
	const shortIdle = 100 * time.Millisecond
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, nil)
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- d.Run(ctx)
	}()

	// Socket is already bound by NewTestDaemon; wait for it to be connectable.
	waitForSocket(t, sockPath, 3*time.Second)

	// No connections: daemon should exit after shortIdle.
	select {
	case err := <-daemonDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not auto-shutdown after idle TTL")
	}

	// Socket file should be removed.
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket file should be removed after auto-shutdown")
	}
}

// TestAutoShutdown_LiveConnBlocksShutdown verifies that when 2 connections are
// active, one exiting does NOT trigger shutdown — the idle timer must only arm
// when the registry actually empties (Count()==0). The daemon must remain live
// while conn 2 is still open, and only shut down after conn 2 also exits.
func TestAutoShutdown_LiveConnBlocksShutdown(t *testing.T) {
	dir := tmpShortDir(t, "block")
	sockPath := codemcp.SocketPath(dir)

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	const shortIdle = 150 * time.Millisecond
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, nil)
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- d.Run(ctx)
	}()
	waitForSocket(t, sockPath, 3*time.Second)

	// Open conn1 (cancels the startup idle timer).
	conn1, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}

	// Open conn2 while conn1 is live.
	conn2, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		conn1.Close()
		t.Fatalf("dial conn2: %v", err)
	}

	// Wait for both connections to register (poll RegistryCount).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.RegistryCount() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := d.RegistryCount(); got != 2 {
		conn1.Close()
		conn2.Close()
		t.Fatalf("expected 2 registered connections, got %d", got)
	}

	// Close conn1 — registry drops to 1, idle timer must NOT arm.
	conn1.Close()

	// Wait longer than shortIdle: daemon must remain live (conn2 still active).
	time.Sleep(shortIdle * 4)
	if !codemcp.IsLive(sockPath) {
		conn2.Close()
		t.Fatal("daemon shut down while conn2 was still active — idle timer armed with live connection")
	}

	// Now close conn2 — registry empties, idle timer arms.
	conn2.Close()

	// Daemon must shut down within a reasonable window after the idle timer fires.
	select {
	case err := <-daemonDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(shortIdle*10 + time.Second):
		t.Fatal("daemon did not auto-shutdown after last connection closed")
	}
}

// TestAutoShutdown_ConnectionCancelsTimer verifies that a connection arriving
// before the idle timer fires cancels the shutdown.
func TestAutoShutdown_ConnectionCancelsTimer(t *testing.T) {
	dir := tmpShortDir(t, "cancel")
	sockPath := codemcp.SocketPath(dir)

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build engine and server before starting the goroutine (see TestAutoShutdown_SocketRemovedAfterIdle).
	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	// NewTestDaemon binds the socket synchronously so polling starts immediately.
	const shortIdle = 200 * time.Millisecond
	// NewTestDaemon binds the socket synchronously; the file exists immediately.
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, nil)
	go func() {
		_ = d.Run(ctx)
	}()

	// Socket file exists from NewTestDaemon; wait for accept loop to start
	// then connect immediately to beat the idle timer.
	waitForSocket(t, sockPath, 3*time.Second)

	// Connect before the idle timer fires.
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Poll until the connection is registered — confirms the daemon accepted it.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.RegistryCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.RegistryCount() < 1 {
		t.Fatal("connection was not registered in time")
	}

	// Daemon should still be alive well past the idle window: we have an active conn.
	// Poll IsLive over 3× the idle period — if it dies, the test fails immediately.
	liveUntil := time.Now().Add(shortIdle * 3)
	for time.Now().Before(liveUntil) {
		if !codemcp.IsLive(sockPath) {
			t.Fatal("daemon should still be live while a connection is open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// TestReaper — clock-injectable reaper
// ---------------------------------------------------------------------------

// TestReaper_ClosesIdleConn verifies the reaper closes idle connections
// using a fake clock (no real sleep).
func TestReaper_ClosesIdleConn(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	reg := codemcp.NewRegistry(clock.Now)

	// Add two connections.
	closedC1 := make(chan struct{})
	closedC2 := make(chan struct{})

	reg.Add("c1")
	reg.Add("c2")

	// Advance 31 minutes: both connections have been idle for 31m (> 30m TTL).
	// Touch c2 now — c2's updatedAt becomes t=31m.
	clock.Advance(31 * time.Minute)
	reg.Touch("c2") // c2 freshened at t=31m; c1 still has updatedAt=0

	// Advance 29 more minutes (total 60m).
	// c1: idle since t=0, now 60m idle → exceeds ConnIdleTTL (30m) → should reap.
	// c2: last touched at t=31m, now 29m idle → under ConnIdleTTL → should NOT reap.
	clock.Advance(29 * time.Minute)

	closers := map[string]func(){
		"c1": func() { close(closedC1) },
		"c2": func() { close(closedC2) },
	}
	idle := reg.Idle(codemcp.ConnIdleTTL)
	for _, id := range idle {
		if fn, ok := closers[id]; ok {
			fn()
		}
	}

	select {
	case <-closedC1:
		// expected
	default:
		t.Fatal("c1 should have been reaped (idle > connIdleTTL)")
	}
	select {
	case <-closedC2:
		t.Fatal("c2 should NOT have been reaped (it was recently touched)")
	default:
		// expected
	}
}

// ---------------------------------------------------------------------------
// Real-socket end-to-end test
// ---------------------------------------------------------------------------

// TestE2E_DaemonProxyMCPInitialize is the one real-socket integration test.
// It starts an in-process daemon bound to a real unix socket, connects an MCP
// client through the socket, calls initialize, and verifies a tool call works.
func TestE2E_DaemonProxyMCPInitialize(t *testing.T) {
	dir := tmpShortDir(t, "e2e")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Index a tiny fixture so the engine is non-empty.
	eng, fileCount := newTestEngine(t, map[string]string{
		"greet.go": `package main

func Greet(name string) string {
	return "hello " + name
}`,
	})
	defer eng.Close()

	srv := codemcp.NewServer(eng, fileCount)

	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Start daemon.
	daemonDone := make(chan error, 1)
	go func() {
		ln, err := net.Listen("unix", sockPath)
		if err != nil {
			daemonDone <- err
			return
		}
		daemonDone <- codemcp.RunAcceptLoop(ctx, ln, srv, sockPath)
	}()

	waitForSocketLive(t, sockPath, 5*time.Second)

	// Connect MCP client directly to the socket.
	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

	// Wrap as IOTransport for the SDK client.
	clientTransport := &sdk.IOTransport{
		Reader: conn,
		Writer: conn,
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "e2e-test", Version: "1"}, nil)
	sess, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer sess.Close()

	// Verify tools/list returns our atomic_code_* tools.
	res, err := sess.ListTools(ctx, &sdk.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	found := false
	for _, tool := range res.Tools {
		if tool.Name == "atomic_code_search" {
			found = true
			break
		}
	}
	if !found {
		names := make([]string, len(res.Tools))
		for i, tr := range res.Tools {
			names[i] = tr.Name
		}
		t.Fatalf("atomic_code_search not in tool list: %v", names)
	}

	// Call atomic_code_search and verify non-error response.
	toolRes, err := sess.CallTool(ctx, &sdk.CallToolParams{
		Name:      "atomic_code_search",
		Arguments: map[string]any{"query": "Greet"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if toolRes.IsError {
		t.Fatalf("tool returned error: %v", toolRes)
	}
	// Find text content and verify it contains something about Greet.
	var text string
	for _, c := range toolRes.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if text == "" {
		t.Fatal("expected non-empty search result")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// fakeClock is a simple injectable clock for testing.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// tmpShortDir returns a short path in /tmp to stay within the ~104-char
// unix socket path limit on macOS.
func tmpShortDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atmc-"+prefix+"-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// newEmptyEngine creates an initialised (empty) engine in dir.
func newEmptyEngine(t *testing.T, dir string) *engine.Engine {
	t.Helper()
	eng, err := engine.New(dir)
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	ctx := context.Background()
	if err := eng.Init(ctx); err != nil {
		eng.Close()
		t.Fatalf("eng.Init: %v", err)
	}
	return eng
}

// startInProcessDaemon starts an in-process daemon on projectRoot's socket.
// Used as the spawn seam stub in auto-start tests so no subprocess is needed.
// Assumes the canonical db location: <projectRoot>/.claude/.atomic-index/atomic.db.
func startInProcessDaemon(t *testing.T, projectRoot string) error {
	t.Helper()
	dbPath := filepath.Join(projectRoot, ".claude", ".atomic-index", "atomic.db")
	return startInProcessDaemonWithDB(t, projectRoot, dbPath)
}

// startInProcessDaemonWithDB starts an in-process daemon with explicit source+db.
// Used as the spawn seam stub in auto-start tests so no subprocess is needed.
func startInProcessDaemonWithDB(t *testing.T, sourceRoot, dbPath string) error {
	t.Helper()
	sockPath := codemcp.SocketPathFromDB(dbPath)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		ln.Close()
		_ = os.Remove(sockPath)
	})

	eng := newEmptyEngine(t, sourceRoot)
	t.Cleanup(func() { eng.Close() })

	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	go func() {
		_ = codemcp.RunAcceptLoop(ctx, ln, srv, sockPath)
	}()

	return nil
}

// waitForSocket polls until the socket file exists or deadline expires.
// It checks for file existence rather than dialing to avoid creating MCP
// sessions that would interfere with idle-shutdown tests.
func waitForSocket(t *testing.T, sockPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear within %v", sockPath, timeout)
}

// waitForSocketLive polls until the socket is connectable or deadline expires.
// Use this when you need to confirm the accept loop is running (not just the
// file exists). This WILL create a connection — do not use in idle-shutdown tests.
func waitForSocketLive(t *testing.T, sockPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if codemcp.IsLive(sockPath) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("socket %s did not become live within %v", sockPath, timeout)
}
