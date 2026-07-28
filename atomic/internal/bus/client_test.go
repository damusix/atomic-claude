package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testBusHome creates a short, /tmp-rooted (not t.TempDir()) directory for
// tests that bind a real Unix domain socket at SocketPath(home)/
// LockPath(home). t.TempDir() embeds the full test name and can produce a
// path well over the ~104-108 byte sun_path limit on macOS/Linux — the
// exact reason daemon_test.go's testListener avoids it too. Falls back to
// t.TempDir() if /tmp is unavailable, e.g. a sandboxed CI environment.
func testBusHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "atomicbus-home")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startTestDaemon binds a real listener at SocketPath(home) and runs the
// committed Serve loop against it in the background, for the life of the
// test. Matches daemon_test.go's startServe (cancel + bounded wait on
// cleanup) but binds the fixed production socket path instead of an
// arbitrary one, since EnsureDaemon dials exactly SocketPath(home). Also
// mirrors serveAction's own rehydrate-before-serve step (action.go), so a
// test that uses this as its Ensurer.Spawn (client_test.go's countingSpawn)
// exercises the same "whole roster comes back on respawn" behavior a real
// `atomic bus serve` gives production callers — a bare NewHub(home) here
// would silently drop that and reintroduce the per-session recovery gap
// this replaced.
func startTestDaemon(t *testing.T, home string) error {
	t.Helper()

	ln, err := net.Listen("unix", SocketPath(home))
	if err != nil {
		return err
	}
	hub := NewHub(home)
	if st, err := Load(home); err == nil {
		hub.Rehydrate(st)
	}
	startServe(t, ln, hub, 0)
	return nil
}

// leaveStaleSocket binds and then closes a listener at SocketPath(home)
// with SetUnlinkOnClose(false), reproducing exactly the scenario the
// brief's stale-socket recovery step targets: a socket file left behind
// by a crashed daemon, present on disk but refusing every connection.
// Plain Close on a *net.UnixListener created via net.Listen unlinks the
// socket file automatically (see daemon.go's Serve doc) — this is why the
// unlink step is disabled first.
func leaveStaleSocket(t *testing.T, home string) {
	t.Helper()

	if err := EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	ln, err := net.Listen("unix", SocketPath(home))
	if err != nil {
		t.Fatalf("bind stale socket: %v", err)
	}
	ul, ok := ln.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", ln)
	}
	ul.SetUnlinkOnClose(false)
	if err := ul.Close(); err != nil {
		t.Fatalf("close stale listener: %v", err)
	}
}

// serveFakePingVersion is a minimal stand-in for the real daemon, used
// only to make ping report an arbitrary ProtocolVersion — something the
// committed daemon.go always reports truthfully, so a version-skew
// scenario can only be reproduced against a fake.
func serveFakePingVersion(ln net.Listener, version int) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			var req Request
			if err := json.NewDecoder(c).Decode(&req); err != nil {
				return
			}
			payload, _ := json.Marshal(struct {
				Version int `json:"version"`
			}{Version: version})
			_ = json.NewEncoder(c).Encode(Response{OK: true, Payload: payload})
		}(conn)
	}
}

func countingSpawn(t *testing.T, count *int32) func(home string) error {
	t.Helper()
	return func(home string) error {
		atomic.AddInt32(count, 1)
		return startTestDaemon(t, home)
	}
}

// --- Client.Do / Subscribe / Close ---

func TestClient_Do_PingRoundTrip(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)

	conn, err := net.DialTimeout("unix", ln.Addr().String(), wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newClient(conn, wireTimeout)
	defer client.Close()

	resp, err := client.Do(Request{Op: OpPing})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ping failed: %s", resp.Error)
	}
	var payload struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Version != ProtocolVersion {
		t.Errorf("Version = %d, want %d", payload.Version, ProtocolVersion)
	}
}

// TestClient_Do_FailedResponseMapsToErrorWithDaemonsCode proves the
// contract in client.go's Do doc: the daemon assigns the exit code
// (protocol.go's Response.Code), and Do must surface that exact code as a
// *bus.Error rather than re-deriving one from the error text.
func TestClient_Do_FailedResponseMapsToErrorWithDaemonsCode(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)

	conn, err := net.DialTimeout("unix", ln.Addr().String(), wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newClient(conn, wireTimeout)
	defer client.Close()

	resp, err := client.Do(Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "not-a-kind", Session: "sess-1"})
	if err == nil {
		t.Fatal("expected an error for an invalid kind")
	}
	if resp.OK {
		t.Fatal("expected resp.OK = false")
	}
	var busErr *Error
	if !errors.As(err, &busErr) {
		t.Fatalf("expected *bus.Error, got %T: %v", err, err)
	}
	if busErr.Code != ExitUsage {
		t.Fatalf("Code = %d, want ExitUsage (%d)", busErr.Code, ExitUsage)
	}
	if resp.Code != ExitUsage {
		t.Fatalf("resp.Code = %d, want ExitUsage (%d)", resp.Code, ExitUsage)
	}
}

// TestClient_Subscribe_DeliversFramePublishedAfterSubscription is the
// checkpoint's explicit Subscribe deliverable: subscribe, publish from a
// second connection, read the delivered frame under a bounded select —
// never an unbounded read.
func TestClient_Subscribe_DeliversFramePublishedAfterSubscription(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
	addr := ln.Addr().String()

	// Each daemon connection is one-shot — the daemon closes it after a
	// single non-subscription round trip (daemon.go's handleConn) — so
	// join and send below each use their own fresh connection via
	// daemon_test.go's dialAndDo helper; only the Subscribe call under
	// test gets a dedicated, persistent Client.
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: KindAgent, Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}

	subConn, err := net.DialTimeout("unix", addr, wireTimeout)
	if err != nil {
		t.Fatalf("dial subscriber: %v", err)
	}
	sub := newClient(subConn, wireTimeout)
	defer sub.Close()

	ch, err := sub.Subscribe(Request{Op: OpRecv, Room: "potato", Follow: true})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", Text: "hello"}); !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	select {
	case env := <-ch:
		if env.Text != "hello" {
			t.Fatalf("Text = %q, want %q", env.Text, "hello")
		}
	case <-time.After(wireTimeout):
		t.Fatal("timed out waiting for the frame published after subscription")
	}
}

func TestClient_Subscribe_ChannelClosesWhenClientCloses(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)

	conn, err := net.DialTimeout("unix", ln.Addr().String(), wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newClient(conn, wireTimeout)

	ch, err := client.Subscribe(Request{Op: OpTail, Rooms: []string{"potato"}})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the channel to be closed, got an envelope")
		}
	case <-time.After(wireTimeout):
		t.Fatal("channel did not close within the bounded wait after Close")
	}
}

func TestClient_Close_IsSafeToCallTwice(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)

	conn, err := net.DialTimeout("unix", ln.Addr().String(), wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newClient(conn, wireTimeout)

	if err := client.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// --- EnsureDaemon ---

func TestEnsureDaemon_ColdSingleCaller_SpawnsAndConnects(t *testing.T) {
	home := testBusHome(t)
	var spawnCount int32
	ens := Ensurer{
		Spawn:        countingSpawn(t, &spawnCount),
		DialTimeout:  500 * time.Millisecond,
		SpawnWait:    2 * time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	client, err := ens.EnsureDaemon(home)
	if err != nil {
		t.Fatalf("EnsureDaemon: %v", err)
	}
	defer client.Close()

	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times, want exactly 1", got)
	}

	resp, err := client.Do(Request{Op: OpPing})
	if err != nil || !resp.OK {
		t.Fatalf("ping on the returned client failed: err=%v resp=%+v", err, resp)
	}
}

// TestEnsureDaemon_ConcurrentFromCold_SpawnsExactlyOneDaemon is the
// marquee test of this checkpoint: N callers race EnsureDaemon from a home
// with nothing in it. Each call reaches acquireLock through its own fresh
// os.OpenFile, so this genuinely exercises flock contention per POSIX's
// per-open-file-description semantics — not a shared fd, not a Go mutex
// standing in for the lock. Exactly one of them may spawn.
func TestEnsureDaemon_ConcurrentFromCold_SpawnsExactlyOneDaemon(t *testing.T) {
	home := testBusHome(t)
	var spawnCount int32
	ens := Ensurer{
		Spawn:        countingSpawn(t, &spawnCount),
		DialTimeout:  time.Second,
		SpawnWait:    3 * time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	const n = 12
	var wg sync.WaitGroup
	clients := make([]*Client, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			clients[i], errs[i] = ens.EnsureDaemon(home)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("EnsureDaemon[%d]: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times across %d concurrent callers, want exactly 1", got, n)
	}

	// "Connects to the same daemon": every client's ping must report the
	// identical Started timestamp — a second daemon bound to the same
	// socket path is impossible (net.Listen fails with "address already
	// in use"), so this also verifies no client silently talked to
	// nothing and no client's spawn attempt raced past the address check.
	var wantStarted time.Time
	for i, c := range clients {
		resp, err := c.Do(Request{Op: OpPing})
		if err != nil || !resp.OK {
			t.Fatalf("ping on client[%d] failed: err=%v resp=%+v", i, err, resp)
		}
		var payload struct {
			Started time.Time `json:"started"`
		}
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			t.Fatalf("unmarshal ping payload[%d]: %v", i, err)
		}
		if i == 0 {
			wantStarted = payload.Started
		} else if !payload.Started.Equal(wantStarted) {
			t.Fatalf("client[%d] Started = %v, want %v (all clients must reach the same daemon)", i, payload.Started, wantStarted)
		}
		c.Close()
	}
}

// TestEnsureDaemon_LockLoserBlocksThenWakesAndConnectsToSameDaemon
// deterministically proves the "loser blocks, wakes, finds the socket,
// connects" success criterion, rather than relying on timing luck in the
// N-way concurrent test above: the winner is held inside its locked
// spawn step until the test releases it, and the loser — started only
// after the winner has had time to acquire the lock — must still be
// blocked at that point.
func TestEnsureDaemon_LockLoserBlocksThenWakesAndConnectsToSameDaemon(t *testing.T) {
	home := testBusHome(t)
	var spawnCount int32
	proceed := make(chan struct{})
	spawn := func(spawnHome string) error {
		atomic.AddInt32(&spawnCount, 1)
		<-proceed // held here until the test lets the winner finish
		return startTestDaemon(t, spawnHome)
	}
	ens := Ensurer{
		Spawn:        spawn,
		DialTimeout:  time.Second,
		SpawnWait:    3 * time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	type result struct {
		client *Client
		err    error
	}
	winnerDone := make(chan result, 1)
	go func() {
		c, err := ens.EnsureDaemon(home)
		winnerDone <- result{c, err}
	}()

	// Give the winner time to acquire the lock and park inside Spawn.
	time.Sleep(100 * time.Millisecond)

	loserStarted := make(chan struct{})
	loserDone := make(chan result, 1)
	go func() {
		close(loserStarted)
		c, err := ens.EnsureDaemon(home)
		loserDone <- result{c, err}
	}()
	<-loserStarted
	time.Sleep(50 * time.Millisecond) // let the loser reach acquireLock and block

	select {
	case <-loserDone:
		t.Fatal("loser returned before the winner released the lock — the lock did not serialize the two callers")
	default:
		// expected: still blocked
	}

	close(proceed) // let the winner finish spawning, connect, and release the lock

	winner := <-winnerDone
	if winner.err != nil {
		t.Fatalf("winner EnsureDaemon: %v", winner.err)
	}
	defer winner.client.Close()

	var loser result
	select {
	case loser = <-loserDone:
	case <-time.After(wireTimeout):
		t.Fatal("loser did not wake up and return after the winner released the lock")
	}
	if loser.err != nil {
		t.Fatalf("loser EnsureDaemon: %v", loser.err)
	}
	defer loser.client.Close()

	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times, want exactly 1 (the loser must not have spawned its own daemon)", got)
	}

	winnerResp, err := winner.client.Do(Request{Op: OpPing})
	if err != nil || !winnerResp.OK {
		t.Fatalf("ping on winner client failed: err=%v resp=%+v", err, winnerResp)
	}
	loserResp, err := loser.client.Do(Request{Op: OpPing})
	if err != nil || !loserResp.OK {
		t.Fatalf("ping on loser client failed: err=%v resp=%+v", err, loserResp)
	}
	var winnerPayload, loserPayload struct {
		Started time.Time `json:"started"`
	}
	if err := json.Unmarshal(winnerResp.Payload, &winnerPayload); err != nil {
		t.Fatalf("unmarshal winner payload: %v", err)
	}
	if err := json.Unmarshal(loserResp.Payload, &loserPayload); err != nil {
		t.Fatalf("unmarshal loser payload: %v", err)
	}
	if !winnerPayload.Started.Equal(loserPayload.Started) {
		t.Fatalf("winner Started = %v, loser Started = %v, want equal (same daemon)", winnerPayload.Started, loserPayload.Started)
	}
}

// TestEnsureDaemon_StaleSocket_UnlinkedRespawnedConnected_SpawnOnce proves
// the stale-socket recovery path: a socket file present with nothing
// listening (a crashed daemon's leftover) is unlinked, the daemon is
// respawned once, and the caller connects — spawn invoked exactly once.
func TestEnsureDaemon_StaleSocket_UnlinkedRespawnedConnected_SpawnOnce(t *testing.T) {
	home := testBusHome(t)
	leaveStaleSocket(t, home)

	var spawnCount int32
	ens := Ensurer{
		Spawn:        countingSpawn(t, &spawnCount),
		DialTimeout:  500 * time.Millisecond,
		SpawnWait:    2 * time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	client, err := ens.EnsureDaemon(home)
	if err != nil {
		t.Fatalf("EnsureDaemon: %v", err)
	}
	defer client.Close()

	if got := atomic.LoadInt32(&spawnCount); got != 1 {
		t.Fatalf("spawn invoked %d times, want exactly 1", got)
	}

	resp, err := client.Do(Request{Op: OpPing})
	if err != nil || !resp.OK {
		t.Fatalf("ping on the returned client failed: err=%v resp=%+v", err, resp)
	}
}

// TestEnsureDaemon_PersistentFailure_ExitsSixAfterExactlyOneRetry_NoLoop
// pins the "do not loop" property explicitly by counting spawn attempts:
// a Spawn that never actually brings up a working listener must be called
// exactly maxSpawnAttempts times (the initial attempt plus one retry),
// never a third time, and the final failure must carry exit code 6.
func TestEnsureDaemon_PersistentFailure_ExitsSixAfterExactlyOneRetry_NoLoop(t *testing.T) {
	home := testBusHome(t)
	leaveStaleSocket(t, home)

	var spawnCount int32
	ens := Ensurer{
		Spawn: func(spawnHome string) error {
			atomic.AddInt32(&spawnCount, 1)
			return nil // "starts" but never actually opens the socket
		},
		DialTimeout:  100 * time.Millisecond,
		SpawnWait:    80 * time.Millisecond,
		PollInterval: 10 * time.Millisecond,
	}

	client, err := ens.EnsureDaemon(home)
	if err == nil {
		client.Close()
		t.Fatal("expected EnsureDaemon to fail when the daemon never comes up")
	}
	var busErr *Error
	if !errors.As(err, &busErr) {
		t.Fatalf("expected *bus.Error, got %T: %v", err, err)
	}
	if busErr.Code != ExitUnreachable {
		t.Fatalf("Code = %d, want ExitUnreachable (%d)", busErr.Code, ExitUnreachable)
	}
	if got := atomic.LoadInt32(&spawnCount); got != maxSpawnAttempts {
		t.Fatalf("spawn invoked %d times, want exactly %d (no loop past the single retry)", got, maxSpawnAttempts)
	}
}

// TestEnsureDaemon_VersionMismatch_ExitsSix_NamesBothVersionsAndRemedy
// pins the version-skew refusal: a daemon reporting a different
// ProtocolVersion must be refused (exit 6) without ever spawning, and the
// error must name both versions plus the remedy — the message is the
// entire actionable output here.
func TestEnsureDaemon_VersionMismatch_ExitsSix_NamesBothVersionsAndRemedy(t *testing.T) {
	home := testBusHome(t)
	if err := EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	const runningVersion = ProtocolVersion + 1

	ln, err := net.Listen("unix", SocketPath(home))
	if err != nil {
		t.Fatalf("bind fake daemon: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go serveFakePingVersion(ln, runningVersion)

	var spawnCount int32
	ens := Ensurer{
		Spawn: func(string) error {
			atomic.AddInt32(&spawnCount, 1)
			return fmt.Errorf("must not be called on version skew")
		},
		DialTimeout:  500 * time.Millisecond,
		SpawnWait:    time.Second,
		PollInterval: 10 * time.Millisecond,
	}

	client, err := ens.EnsureDaemon(home)
	if err == nil {
		client.Close()
		t.Fatal("expected a version-skew error")
	}
	var busErr *Error
	if !errors.As(err, &busErr) {
		t.Fatalf("expected *bus.Error, got %T: %v", err, err)
	}
	if busErr.Code != ExitUnreachable {
		t.Fatalf("Code = %d, want ExitUnreachable (%d)", busErr.Code, ExitUnreachable)
	}
	msg := err.Error()
	if !strings.Contains(msg, fmt.Sprintf("v%d", runningVersion)) {
		t.Errorf("message %q does not name the running version v%d", msg, runningVersion)
	}
	if !strings.Contains(msg, fmt.Sprintf("v%d", ProtocolVersion)) {
		t.Errorf("message %q does not name the client version v%d", msg, ProtocolVersion)
	}
	if !strings.Contains(msg, "atomic bus serve --stop") {
		t.Errorf("message %q does not name the remedy", msg)
	}
	if got := atomic.LoadInt32(&spawnCount); got != 0 {
		t.Fatalf("spawn invoked %d times on version skew, want 0 (must refuse, never respawn)", got)
	}
}
