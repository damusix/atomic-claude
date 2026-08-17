// A fake clock drives the registry, reaper, and auto-shutdown, so timing
// assertions are instant. Auto-start tests inject an in-process spawn stub
// rather than forking a subprocess.
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
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

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
	if codemcp.SyncInterval != 10*time.Second {
		t.Errorf("SyncInterval = %v, want 10s", codemcp.SyncInterval)
	}
}

func TestSocketAndLockPath_UnderNonDefaultHarnessDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".pi")
	defer restore()

	root := "/repo"
	wantSock := filepath.Join(root, ".pi", ".atomic-index", "atomic.mcp.sock")
	wantLock := filepath.Join(root, ".pi", ".atomic-index", "atomic.mcp.lock")

	if got := codemcp.SocketPath(root); got != wantSock {
		t.Errorf("SocketPath = %q, want %q", got, wantSock)
	}
	if got := codemcp.LockPath(root); got != wantLock {
		t.Errorf("LockPath = %q, want %q", got, wantLock)
	}
}

// Without this, a no-op syncLoop or a disabled poller would pass silently.
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

	// Short idle keeps the daemon alive with zero connections.
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

	select {
	case <-syncCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("syncFn was not called within 3s — poller did not fire")
	}
}

// A poller outliving the daemon would leak and could sync a closed engine. A
// syncFn call between cancel and Run.s return is legitimate; one after Run has
// returned is the leak.
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

	var daemonStopped atomic.Bool
	var syncAfterDaemonStopped atomic.Bool
	syncFn := func(_ context.Context) error {
		if daemonStopped.Load() {
			syncAfterDaemonStopped.Store(true)
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

	time.Sleep(shortSync * 3)

	cancel()
	select {
	case <-daemonDone:
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop after ctx cancel")
	}

	// Past this point the WaitGroup has drained; nothing may fire.
	daemonStopped.Store(true)

	time.Sleep(shortSync * 3)
	if syncAfterDaemonStopped.Load() {
		t.Fatal("syncFn was called after daemon fully stopped — poller goroutine leaked past shutdown")
	}
}

// --no-watch must disable the poller outright: a zero-interval ticker would
// otherwise fire immediately and bypass the guard.
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

	reg.Add("c1")
	reg.Add("c2")
	clock.Advance(15 * time.Minute)
	reg.Touch("c2")

	clock.Advance(20 * time.Minute)

	idle := reg.Idle(codemcp.ConnIdleTTL)
	if len(idle) != 1 || idle[0] != "c1" {
		t.Fatalf("expected only c1 idle, got %v", idle)
	}
}

func TestAutoStart_SpawnCalledOnce(t *testing.T) {
	dir := tmpShortDir(t, "as")
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")

	var spawnCount atomic.Int32
	stub := func(sourceRoot, db string, _ codemcp.WatchOptions) error {
		spawnCount.Add(1)
		return startInProcessDaemonWithDB(t, sourceRoot, db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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

func TestAutoStart_StaleSocketRemoved(t *testing.T) {
	dir := tmpShortDir(t, "stale")
	dbPath := filepath.Join(dir, ".claude", ".atomic-index", "atomic.db")

	sockPath := codemcp.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Written by hand: on macOS Listen+Close removes the file, so a crashed
	// daemon.s leftover has to be simulated.
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

	const longIdle = 30 * time.Second
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, longIdle, longIdle, 0, nil)
	go func() {
		_ = d.Run(ctx)
	}()
	waitForSocket(t, sockPath, 3*time.Second)

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

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if d.RegistryCount() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Two live connections on one engine is the warm-reuse proof.
	if got := d.RegistryCount(); got != 2 {
		t.Fatalf("RegistryCount = %d, want 2 (warm reuse: both connections should be registered)", got)
	}

	if !codemcp.IsLive(sockPath) {
		t.Fatal("daemon should still be live with 2 clients connected")
	}
}

func TestAutoShutdown_SocketRemovedAfterIdle(t *testing.T) {
	dir := tmpShortDir(t, "idle")
	sockPath := codemcp.SocketPath(dir)

	if err := os.MkdirAll(filepath.Dir(sockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Built before the goroutine so Listen happens promptly inside the deadline.
	eng := newEmptyEngine(t, dir)
	defer eng.Close()
	stats, _ := eng.GetStats(ctx)
	srv := codemcp.NewServer(eng, stats.FileCount)

	const shortIdle = 100 * time.Millisecond
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, nil)
	daemonDone := make(chan error, 1)
	go func() {
		daemonDone <- d.Run(ctx)
	}()

	waitForSocket(t, sockPath, 3*time.Second)

	select {
	case err := <-daemonDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not auto-shutdown after idle TTL")
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Fatal("socket file should be removed after auto-shutdown")
	}
}

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

	conn1, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}

	conn2, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		conn1.Close()
		t.Fatalf("dial conn2: %v", err)
	}

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

	// Registry drops to 1, so the idle timer must not arm.
	conn1.Close()

	time.Sleep(shortIdle * 4)
	if !codemcp.IsLive(sockPath) {
		conn2.Close()
		t.Fatal("daemon shut down while conn2 was still active — idle timer armed with live connection")
	}

	conn2.Close()

	select {
	case err := <-daemonDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("daemon exited with error: %v", err)
		}
	case <-time.After(shortIdle*10 + time.Second):
		t.Fatal("daemon did not auto-shutdown after last connection closed")
	}
}

func TestAutoShutdown_ConnectionCancelsTimer(t *testing.T) {
	dir := tmpShortDir(t, "cancel")
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

	const shortIdle = 200 * time.Millisecond
	d := codemcp.NewTestDaemon(sockPath, srv, time.Now, shortIdle, shortIdle, 0, nil)
	go func() {
		_ = d.Run(ctx)
	}()

	waitForSocket(t, sockPath, 3*time.Second)

	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

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

	// An active connection must hold the daemon open past the idle window.
	liveUntil := time.Now().Add(shortIdle * 3)
	for time.Now().Before(liveUntil) {
		if !codemcp.IsLive(sockPath) {
			t.Fatal("daemon should still be live while a connection is open")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReaper_ClosesIdleConn(t *testing.T) {
	clock := &fakeClock{t: time.Now()}
	reg := codemcp.NewRegistry(clock.Now)

	closedC1 := make(chan struct{})
	closedC2 := make(chan struct{})

	reg.Add("c1")
	reg.Add("c2")

	clock.Advance(31 * time.Minute)
	reg.Touch("c2") // c2 freshened at t=31m; c1 still has updatedAt=0

	// c1 is now 60m idle and c2 only 29m, straddling the 30m TTL.
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

// The one test that drives a real unix socket end to end.
func TestE2E_DaemonProxyMCPInitialize(t *testing.T) {
	dir := tmpShortDir(t, "e2e")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial socket: %v", err)
	}
	defer conn.Close()

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

// tmpShortDir keeps socket paths inside the unix sun_path limit.
func tmpShortDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atmc-"+prefix+"-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

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

// The spawn seam stub, so auto-start tests need no subprocess. Assumes the
// canonical db location under projectRoot.
func startInProcessDaemon(t *testing.T, projectRoot string) error {
	t.Helper()
	dbPath := filepath.Join(projectRoot, ".claude", ".atomic-index", "atomic.db")
	return startInProcessDaemonWithDB(t, projectRoot, dbPath)
}

// As above, with source and db given explicitly.
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

// waitForSocket stats rather than dials: dialing would open an MCP session and
// perturb the idle-shutdown tests.
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

// waitForSocketLive proves the accept loop is running, but opens a connection —
// never use it in an idle-shutdown test.
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
