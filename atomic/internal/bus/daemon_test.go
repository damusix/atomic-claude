package bus

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// wireTimeout bounds every wire-level assertion in this file — a bounded
// select on a timeout channel, never an unbounded block, per the atomic-bus
// brief's concurrency success criteria.
const wireTimeout = 2 * time.Second

// testListener binds a unix socket for one test. It deliberately roots the
// socket under /tmp rather than t.TempDir() (which honors $TMPDIR and can
// produce a path exceeding the ~104-108 byte unix socket path limit on
// macOS/Linux) — falling back to t.TempDir() if /tmp is unavailable, e.g.
// a sandboxed CI environment.
func testListener(t *testing.T) net.Listener {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "atomicbus")
	if err != nil {
		dir = t.TempDir()
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}

	ln, err := net.Listen("unix", filepath.Join(dir, "b.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// startServe runs Serve in the background and guarantees it has exited —
// no leaked daemon goroutine — before the test finishes: t.Cleanup
// cancels ctx and then waits (bounded) for Serve to return.
func startServe(t *testing.T, ln net.Listener, hub *Hub) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, nil)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(wireTimeout):
			t.Error("Serve did not exit within the bounded wait after cancellation")
		}
	})
}

// dialAndDoBounded is dialAndDo's actual logic, factored out so it can
// return an error instead of failing a *testing.T — TestDialAndDo_Bounded
// below exercises this exact code path directly, without a nested t.Run
// subtest whose expected failure would otherwise fail the parent test (Go
// always propagates a failed subtest's status to its parent).
func dialAndDoBounded(addr string, req Request, timeout time.Duration) (Response, error) {
	conn, err := net.DialTimeout("unix", addr, timeout)
	if err != nil {
		return Response{}, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("encode request: %w", err)
	}

	// Bounded like every subscription read in this file (readLineBounded):
	// a dispatch regression that never replies must return an error here,
	// not hang the caller. net.Conn (unlike bufio.Reader) supports a read
	// deadline directly, so this needs no goroutine+select.
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return Response{}, fmt.Errorf("set read deadline: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

func dialAndDo(t *testing.T, addr string, req Request) Response {
	t.Helper()

	resp, err := dialAndDoBounded(addr, req, wireTimeout)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return resp
}

// TestDialAndDo_BoundedReadTimesOutRatherThanHanging proves the exact
// code path every one-shot-op test in this file relies on (dialAndDo,
// via dialAndDoBounded) cannot hang the suite: before the read-deadline
// fix, a dispatch regression that accepted a connection and read the
// request but never replied would block Decode forever. Run against a
// bare listener that does exactly that, dialAndDoBounded must return an
// error within roughly its deadline, not hang.
func TestDialAndDo_BoundedReadTimesOutRatherThanHanging(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "atomicbus")
	if err != nil {
		dir = t.TempDir()
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "silent.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req Request
		_ = json.NewDecoder(conn).Decode(&req)
		<-stop // accepted and read the request, then never replies
	}()

	const deadline = 200 * time.Millisecond
	start := time.Now()
	_, err = dialAndDoBounded(ln.Addr().String(), Request{Op: OpPing}, deadline)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error against a server that accepts but never replies")
	}
	if elapsed > deadline+time.Second {
		t.Fatalf("dialAndDoBounded took %s to return, want bounded near its %s deadline (an unbounded read would hang indefinitely)", elapsed, deadline)
	}
}

// dialSubscribe dials, sends req, and reads the opening {"ok":true} frame,
// returning the live connection and a buffered reader positioned right
// after it, ready to read Envelope frames.
func dialSubscribe(t *testing.T, addr string, req Request) (net.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := net.DialTimeout("unix", addr, wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode request: %v", err)
	}

	r := bufio.NewReader(conn)
	line := readLineBounded(t, r, wireTimeout)
	if !line.ok {
		t.Fatalf("timed out waiting for the subscription's opening response")
	}
	var resp Response
	if err := json.Unmarshal(line.data, &resp); err != nil {
		t.Fatalf("unmarshal opening response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("subscribe failed: code=%d error=%s", resp.Code, resp.Error)
	}
	return conn, r
}

type lineResult struct {
	data []byte
	err  error
	ok   bool
}

// readLineBounded reads one newline-delimited frame with a bounded wait,
// so a missed frame fails the calling test with a clear message instead
// of hanging the suite. The read runs in its own goroutine because
// bufio.Reader has no read-deadline support of its own; the goroutine
// unblocks naturally once the connection is closed (by the caller's
// cleanup), so it never leaks past the test.
func readLineBounded(t *testing.T, r *bufio.Reader, timeout time.Duration) lineResult {
	t.Helper()

	ch := make(chan lineResult, 1)
	go func() {
		line, err := r.ReadBytes('\n')
		ch <- lineResult{data: line, err: err, ok: err == nil}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read frame: %v", res.err)
		}
		return res
	case <-time.After(timeout):
		return lineResult{}
	}
}

func readEnvelopeBounded(t *testing.T, r *bufio.Reader) (Envelope, bool) {
	t.Helper()
	res := readLineBounded(t, r, wireTimeout)
	if !res.ok {
		return Envelope{}, false
	}
	var env Envelope
	if err := json.Unmarshal(res.data, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env, true
}

// --- ping ---

func TestServe_Ping_ReturnsVersionPidStarted(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)

	resp := dialAndDo(t, ln.Addr().String(), Request{Op: OpPing})
	if !resp.OK {
		t.Fatalf("ping failed: %s", resp.Error)
	}

	var payload struct {
		Version int       `json:"version"`
		Pid     int       `json:"pid"`
		Started time.Time `json:"started"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal ping payload: %v", err)
	}
	if payload.Version != ProtocolVersion {
		t.Errorf("Version = %d, want %d", payload.Version, ProtocolVersion)
	}
	if payload.Pid != os.Getpid() {
		t.Errorf("Pid = %d, want %d", payload.Pid, os.Getpid())
	}
	if payload.Started.IsZero() {
		t.Error("Started should not be zero")
	}
}

// --- join / leave / who / rooms round trip ---

func TestServe_JoinThenWho_RoundTrip(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	joinResp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Mode: "normal", Session: "sess-1"})
	if !joinResp.OK {
		t.Fatalf("join failed: %s", joinResp.Error)
	}
	var joinPayload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(joinResp.Payload, &joinPayload); err != nil {
		t.Fatalf("unmarshal join payload: %v", err)
	}
	if joinPayload.Name != "backend" {
		t.Fatalf("assigned name = %q, want %q", joinPayload.Name, "backend")
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	if !whoResp.OK {
		t.Fatalf("who failed: %s", whoResp.Error)
	}
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who payload: %v", err)
	}
	if len(whoPayload.Members) != 1 || whoPayload.Members[0].Name != "backend" {
		t.Fatalf("who members = %+v, want one member named backend", whoPayload.Members)
	}

	leaveResp := dialAndDo(t, addr, Request{Op: OpLeave, Room: "potato", Session: "sess-1"})
	if !leaveResp.OK {
		t.Fatalf("leave failed: %s", leaveResp.Error)
	}

	roomsResp := dialAndDo(t, addr, Request{Op: OpRooms})
	if !roomsResp.OK {
		t.Fatalf("rooms failed: %s", roomsResp.Error)
	}
	var roomsPayload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(roomsResp.Payload, &roomsPayload); err != nil {
		t.Fatalf("unmarshal rooms payload: %v", err)
	}
	// A room with no members and no subscribers is dropped, not merely
	// emptied — see room_test.go's TestHub_Leave_LastMemberDropsTheRoom
	// for the Hub-level coverage this wire round trip confirms is actually
	// connected.
	if len(roomsPayload.Rooms) != 0 {
		t.Fatalf("rooms = %+v, want none (potato had its last member leave and should have been dropped)", roomsPayload.Rooms)
	}
}

// TestServe_Close_DropsRoomAndDaemonSideRoomsNoLongerListsIt is the wire
// dispatch's proof that OpClose actually reaches Hub.Close, not merely a
// direct-Hub-call proof (room_test.go already covers Hub.Close's own
// behavior in isolation).
func TestServe_Close_DropsRoomAndDaemonSideRoomsNoLongerListsIt(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	closeResp := dialAndDo(t, addr, Request{Op: OpClose, Room: "potato"})
	if !closeResp.OK {
		t.Fatalf("close failed: %s", closeResp.Error)
	}

	roomsResp := dialAndDo(t, addr, Request{Op: OpRooms})
	if !roomsResp.OK {
		t.Fatalf("rooms failed: %s", roomsResp.Error)
	}
	var roomsPayload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(roomsResp.Payload, &roomsPayload); err != nil {
		t.Fatalf("unmarshal rooms payload: %v", err)
	}
	if len(roomsPayload.Rooms) != 0 {
		t.Fatalf("rooms after close = %+v, want none", roomsPayload.Rooms)
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	if whoResp.OK {
		t.Fatal("expected who on a closed room to fail with ExitNoRoom")
	}
	if whoResp.Code != ExitNoRoom {
		t.Fatalf("who Code = %d, want %d (ExitNoRoom)", whoResp.Code, ExitNoRoom)
	}
}

// TestServe_Who_ReportsHaltedStateAndReason is the wire dispatch's proof
// that OpWho's payload actually carries the room's halt state alongside its
// members.
func TestServe_Who_ReportsHaltedStateAndReason(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpHalt, Room: "potato", Text: "investigating"}); !resp.OK {
		t.Fatalf("halt: %s", resp.Error)
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	if !whoResp.OK {
		t.Fatalf("who failed: %s", whoResp.Error)
	}
	var payload whoJSON
	if err := json.Unmarshal(whoResp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal who payload: %v", err)
	}
	if !payload.Halted || payload.HaltReason != "investigating" {
		t.Fatalf("who payload = %+v, want Halted=true HaltReason=%q", payload, "investigating")
	}
}

// TestServe_Close_SubscriberConnectionEndsCleanly_NoBusySpin is the
// wire-level regression test for a bug this exact change introduced and a
// manual drive of the built binary caught: Hub.Close closes every live
// subscriber's channel (room.go), but this dispatch's own receive loop did
// not check the channel's ok value — a closed channel's receive always
// succeeds immediately with the zero value, so the loop spun forever
// writing empty envelope frames to the connection instead of ending it.
// This proves the connection actually terminates (read returns EOF) shortly
// after the closing envelope, and that no zero-value frame is ever written
// in between.
func TestServe_Close_SubscriberConnectionEndsCleanly_NoBusySpin(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1"}); !resp.OK {
		t.Fatalf("seed join: %s", resp.Error)
	}

	subConn, r := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-1"})
	defer subConn.Close()

	closeResp := dialAndDo(t, addr, Request{Op: OpClose, Room: "potato"})
	if !closeResp.OK {
		t.Fatalf("close failed: %s", closeResp.Error)
	}

	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for the closing envelope")
	}
	if env.Text != "room closed" {
		t.Fatalf("closing envelope Text = %q, want %q", env.Text, "room closed")
	}

	// The connection must end (a clean read error, typically io.EOF) within
	// a bounded window — not spin forever, and not deliver a second,
	// zero-value frame first.
	type readResult struct {
		data []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		data, err := r.ReadBytes('\n')
		done <- readResult{data: data, err: err}
	}()
	select {
	case res := <-done:
		if res.err == nil {
			t.Fatalf("expected the connection to end after the closing envelope, got another frame: %s", res.data)
		}
	case <-time.After(wireTimeout):
		t.Fatal("connection did not end within the bounded wait after the closing envelope — the busy-spin regression this test guards against")
	}
}

// TestServe_Send_FromRepoRealmStampedFromRoster_NotFromRequest is the anti-
// spoof regression the atomic-bus brief requires for the position-derived
// naming entry: sender identity (from/from_kind, and now
// from_repo/from_realm) is assigned server-side from the roster, never read
// from the request — the same invariant a prior finding proved by speaking
// a raw OpSay claiming an existing agent's name. This joins with a real
// position, then sends an OpSend request whose own Repo/Realm fields claim
// something else entirely, and asserts the published envelope's
// FromRepo/FromRealm reflect the roster, not the request.
func TestServe_Send_FromRepoRealmStampedFromRoster_NotFromRequest(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	joinResp := dialAndDo(t, addr, Request{
		Op: OpJoin, Room: "potato", Name: "backend", Kind: KindAgent, Session: "sess-1",
		Repo: "real-repo", Realm: "real-realm",
	})
	if !joinResp.OK {
		t.Fatalf("join failed: %s", joinResp.Error)
	}

	sendResp := dialAndDo(t, addr, Request{
		Op: OpSend, Room: "potato", Session: "sess-1", Text: "hello",
		Repo: "evil-repo", Realm: "evil-realm",
	})
	if !sendResp.OK {
		t.Fatalf("send failed: %s", sendResp.Error)
	}
	var payload struct {
		Envelope Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(sendResp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal send payload: %v", err)
	}
	if payload.Envelope.FromRepo != "real-repo" {
		t.Errorf("FromRepo = %q, want %q (from the roster, not the request's claimed %q)", payload.Envelope.FromRepo, "real-repo", "evil-repo")
	}
	if payload.Envelope.FromRealm != "real-realm" {
		t.Errorf("FromRealm = %q, want %q (from the roster, not the request's claimed %q)", payload.Envelope.FromRealm, "real-realm", "evil-realm")
	}
}

// TestServe_DuplicateNameRejected drives the wire-level dispatch path for
// the same atomic-claim guarantee room_test.go proves at the Hub level —
// this catches a bug in handleJoin's request decoding/response encoding
// that a pure Hub-level test could not see.
func TestServe_DuplicateNameRejected(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	first := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-1"})
	if !first.OK {
		t.Fatalf("first join failed: %s", first.Error)
	}

	second := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-2"})
	if !second.OK {
		t.Fatalf("second join failed: %s", second.Error)
	}
	var secondPayload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(second.Payload, &secondPayload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if secondPayload.Name != "backend-2" {
		t.Fatalf("second join name = %q, want %q", secondPayload.Name, "backend-2")
	}

	third := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-3"})
	if third.OK {
		t.Fatal("expected the third same-name join to fail")
	}
	if third.Code != ExitNameTaken {
		t.Fatalf("third join Code = %d, want ExitNameTaken (%d)", third.Code, ExitNameTaken)
	}
}

// TestServe_ConcurrentJoin_WireLevel is the wire-dispatch counterpart to
// room_test.go's Hub-level concurrent-join proof: N clients race to join
// the same room/name over N separate connections, dispatched by N
// goroutines inside handleConn. The distribution must be identical to the
// Hub-level case — dispatch must not introduce its own race even though
// Hub.Join itself is already proven atomic.
func TestServe_ConcurrentJoin_WireLevel(t *testing.T) {
	const n = 12
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	var wg sync.WaitGroup
	responses := make([]Response, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := "sess-" + string(rune('a'+i))
			responses[i] = dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: session})
		}(i)
	}
	wg.Wait()

	var exactWins, suffixWins, nameTakenFails int
	for _, resp := range responses {
		if !resp.OK {
			if resp.Code == ExitNameTaken {
				nameTakenFails++
			}
			continue
		}
		var payload struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		switch payload.Name {
		case "backend":
			exactWins++
		case "backend-2":
			suffixWins++
		}
	}

	if exactWins != 1 {
		t.Errorf("exact-name winners = %d, want exactly 1", exactWins)
	}
	if suffixWins != 1 {
		t.Errorf("suffix winners = %d, want exactly 1", suffixWins)
	}
	if nameTakenFails != n-2 {
		t.Errorf("ExitNameTaken failures = %d, want %d", nameTakenFails, n-2)
	}
}

// --- send / recv (subscription) ---

// TestServe_Recv_DeliversPublishedEnvelope is the "under one second"
// success criterion end to end over the wire: subscribe with recv, publish
// from a second connection, and read the delivered frame with a bounded
// timeout.
func TestServe_Recv_DeliversPublishedEnvelope(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: "agent", Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join frontend: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-be"}); !resp.OK {
		t.Fatalf("join backend: %s", resp.Error)
	}

	subConn, r := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato"})
	defer subConn.Close()

	sendResp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", To: []string{"backend"}, Text: "please pick this up"})
	if !sendResp.OK {
		t.Fatalf("send failed: %s", sendResp.Error)
	}

	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for the published envelope on recv")
	}
	if env.Text != "please pick this up" {
		t.Fatalf("delivered Text = %q, want %q", env.Text, "please pick this up")
	}
	if env.From != "frontend" || len(env.To) != 1 || env.To[0] != "backend" {
		t.Fatalf("delivered envelope = %+v, want From=frontend To=[backend]", env)
	}
}

// TestServe_Recv_NoBacklogDeliveredForPriorTraffic is the wire-level proof
// of the bug this change fixes: a room with existing traffic must not
// replay any of it to a newly subscribing recv — since("") used to return
// the entire ring, so a recv on a busy room delivered up to 256 old
// messages as Monitor notifications, each evaluated against the reaction
// policy as if freshly arrived. Only a message published after the
// subscription opens may ever arrive.
func TestServe_Recv_NoBacklogDeliveredForPriorTraffic(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: "agent", Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}
	// Traffic published before anyone subscribes.
	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", Text: "before subscribing"}); !resp.OK {
		t.Fatalf("send before: %s", resp.Error)
	}

	subConn, r := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato"})
	defer subConn.Close()

	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", Text: "after subscribing"}); !resp.OK {
		t.Fatalf("send after: %s", resp.Error)
	}

	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for the post-subscribe envelope")
	}
	if env.Text != "after subscribing" {
		t.Fatalf("first delivered envelope Text = %q, want %q (no backlog should have preceded it)", env.Text, "after subscribing")
	}
}

// TestServe_Tail_NeverJoinsAndSeesOthersMail proves the wire-level shape
// of docs/design/atomic-bus.md decision #5: tail does not join (no
// roster entry, confirmed via who) yet still receives traffic addressed
// to other members.
func TestServe_Tail_NeverJoinsAndSeesOthersMail(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: "agent", Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}

	tailConn, r := dialSubscribe(t, addr, Request{Op: OpTail, Rooms: []string{"potato"}})
	defer tailConn.Close()

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 1 {
		t.Fatalf("expected tail to not appear in who, got %d members: %+v", len(whoPayload.Members), whoPayload.Members)
	}

	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", To: []string{"backend"}, Text: "for backend"}); !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for tail to see the published envelope")
	}
	if env.Text != "for backend" {
		t.Fatalf("tail saw Text = %q, want %q", env.Text, "for backend")
	}
}

// TestServe_Recv_SkipSelf_DoesNotReceiveOwnPublish_ButTailDoes is the
// wire-dispatch counterpart to room_test.go's Hub-level self-echo proof: it
// exercises daemon.go's handleConn threading req.Session/req.SkipSelf into
// Hub.Subscribe, a bug a pure Hub-level test (calling h.Subscribe directly)
// cannot see.
func TestServe_Recv_SkipSelf_DoesNotReceiveOwnPublish_ButTailDoes(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-1"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}

	recvConn, recvR := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-1", SkipSelf: true})
	defer recvConn.Close()
	tailConn, tailR := dialSubscribe(t, addr, Request{Op: OpTail, Rooms: []string{"potato"}})
	defer tailConn.Close()

	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-1", Text: "self-published"}); !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	// tail must see it — proves the publish actually happened and tail's
	// own subscription (session "", skipSelf false — daemon.go's OpTail
	// dispatch hardcodes both) is unaffected by anyone else's SkipSelf.
	tailEnv, ok := readEnvelopeBounded(t, tailR)
	if !ok {
		t.Fatal("tail did not receive the published envelope")
	}
	if tailEnv.Text != "self-published" {
		t.Fatalf("tail Text = %q, want %q", tailEnv.Text, "self-published")
	}

	// recv (same session as the sender, SkipSelf set) must not see it: the
	// read must time out, not decode anything.
	if res := readLineBounded(t, recvR, 300*time.Millisecond); res.ok {
		t.Fatalf("recv received its own publish (%q); expected it to be suppressed", res.data)
	}
}

// TestServe_Recv_UnownedSessionClaim_DoesNotFakeFutureMemberLiveness is the
// regression test for the OpRecv session-trust finding: before this fix,
// daemon.go's OpRecv dispatch handed req.Session to Hub.Subscribe verbatim,
// with nothing checking that the connection sending it actually owned it. A
// subscription opened under a session that names nobody yet just sits in
// r.subs; it cannot be told apart from a legitimate one once the real
// member joins under that same session and hasLiveSubscription starts
// matching it, permanently defeating that member's staleness check
// regardless of its own LastSeen. This is the ordering that actually shows
// the difference — claiming a session that already belongs to a room member
// is unaffected by this fix (SessionIsMember honors it, same as before);
// claiming one before the room has ever heard of it is exactly what gets
// downgraded to anonymous.
func TestServe_Recv_UnownedSessionClaim_DoesNotFakeFutureMemberLiveness(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	hub.SetClock(clock.Now)
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	// A subscription claims "sess-victim" before anyone by that session has
	// joined "potato" at all — an unowned claim, whether malicious or a
	// stray script bug. The connection is left open for the rest of the
	// test, never unsubscribed.
	subConn, _ := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Session: "sess-victim"})
	defer subConn.Close()

	// "victim" now joins for real, legitimately claiming the same session
	// string the stray subscription above already claimed.
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "victim", Kind: "agent", Session: "sess-victim"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}

	clock.Advance(staleThreshold + time.Second)

	pruneResp := dialAndDo(t, addr, Request{Op: OpPrune, Room: "potato"})
	if !pruneResp.OK {
		t.Fatalf("prune failed: %s", pruneResp.Error)
	}
	var payload struct {
		Removed []string `json:"removed"`
	}
	if err := json.Unmarshal(pruneResp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal prune payload: %v", err)
	}
	if len(payload.Removed) != 1 || payload.Removed[0] != "victim" {
		t.Fatalf("removed = %v, want [victim] — the unowned-session subscription must not have kept it artificially live", payload.Removed)
	}
}

// TestServe_Prune_RemovesStaleMember_ReportsRemovedInPayload is the
// wire-dispatch counterpart to room_test.go's Hub.Prune proof: OpPrune must
// decode, dispatch, and encode correctly, not merely exist as a Hub method.
func TestServe_Prune_RemovesStaleMember_ReportsRemovedInPayload(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	hub.SetClock(clock.Now)
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "ghost", Kind: "agent", Session: "sess-ghost"}); !resp.OK {
		t.Fatalf("join ghost: %s", resp.Error)
	}
	clock.Advance(staleThreshold + time.Second)

	pruneResp := dialAndDo(t, addr, Request{Op: OpPrune, Room: "potato"})
	if !pruneResp.OK {
		t.Fatalf("prune failed: %s", pruneResp.Error)
	}
	var payload struct {
		Removed []string `json:"removed"`
	}
	if err := json.Unmarshal(pruneResp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal prune payload: %v", err)
	}
	if len(payload.Removed) != 1 || payload.Removed[0] != "ghost" {
		t.Fatalf("removed = %v, want [ghost]", payload.Removed)
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 0 {
		t.Fatalf("members after prune = %+v, want none", whoPayload.Members)
	}
}

// TestServe_Resume_EmptyText_PublishesDefaultBody_OverTheWire is the
// wire-level regression test for finding 4: resumeAction never sent a Text
// field at all, so req.Text was always "" on the daemon side.
func TestServe_Resume_EmptyText_PublishesDefaultBody_OverTheWire(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-1"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}
	subConn, r := dialSubscribe(t, addr, Request{Op: OpTail, Rooms: []string{"potato"}})
	defer subConn.Close()

	if resp := dialAndDo(t, addr, Request{Op: OpHalt, Room: "potato"}); !resp.OK {
		t.Fatalf("halt: %s", resp.Error)
	}
	readEnvelopeBounded(t, r) // the halt control envelope

	if resp := dialAndDo(t, addr, Request{Op: OpResume, Room: "potato"}); !resp.OK {
		t.Fatalf("resume: %s", resp.Error)
	}
	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for the resume control envelope")
	}
	if env.Text == "" {
		t.Fatal("resume published an empty-body envelope over the wire")
	}
}

// --- halt / resume over the wire ---

func TestServe_Halt_BlocksAgentSend_HumanSendStillSucceeds_ResumeRestores(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-agent"}); !resp.OK {
		t.Fatalf("join agent: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "operator", Kind: "human", Session: "sess-human"}); !resp.OK {
		t.Fatalf("join human: %s", resp.Error)
	}

	haltResp := dialAndDo(t, addr, Request{Op: OpHalt, Room: "potato", Text: "stop"})
	if !haltResp.OK {
		t.Fatalf("halt failed: %s", haltResp.Error)
	}

	agentSend := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-agent", Text: "still going"})
	if agentSend.OK {
		t.Fatal("expected agent send into a halted room to fail")
	}
	if agentSend.Code != ExitHalted {
		t.Fatalf("agent send Code = %d, want ExitHalted (%d)", agentSend.Code, ExitHalted)
	}

	humanSend := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-human", Text: "hold on"})
	if !humanSend.OK {
		t.Fatalf("expected human send into a halted room to succeed, got: %s", humanSend.Error)
	}

	resumeResp := dialAndDo(t, addr, Request{Op: OpResume, Room: "potato"})
	if !resumeResp.OK {
		t.Fatalf("resume failed: %s", resumeResp.Error)
	}

	agentSendAfterResume := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-agent", Text: "resumed"})
	if !agentSendAfterResume.OK {
		t.Fatalf("expected agent send to succeed after resume, got: %s", agentSendAfterResume.Error)
	}
}

// --- say over the wire ---

// TestServe_Say_PublishesAsHumanWithoutJoining proves say's daemon-side
// contract directly: no join precedes it, yet the published envelope's
// FromKind is human and `who` gains no roster member for it.
func TestServe_Say_PublishesAsHumanWithoutJoining(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-agent"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}

	sayResp := dialAndDo(t, addr, Request{Op: OpSay, Room: "potato", Name: "human", Kind: "human", Text: "operator speaking"})
	if !sayResp.OK {
		t.Fatalf("say failed: %s", sayResp.Error)
	}
	var payload struct {
		Envelope Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(sayResp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal say response: %v", err)
	}
	if payload.Envelope.FromKind != KindHuman {
		t.Errorf("FromKind = %q, want %q", payload.Envelope.FromKind, KindHuman)
	}

	whoResp := dialAndDo(t, addr, Request{Op: OpWho, Room: "potato"})
	var whoPayload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(whoResp.Payload, &whoPayload); err != nil {
		t.Fatalf("unmarshal who: %v", err)
	}
	if len(whoPayload.Members) != 1 {
		t.Fatalf("expected say to not add a roster member, got %d: %+v", len(whoPayload.Members), whoPayload.Members)
	}
}

// TestServe_Say_BypassesHalt is the wire-level half of the say/halt
// asymmetry: OpSay must succeed into a halted room exactly like a joined
// human's OpSend already does (TestServe_Halt_...ResumeRestores above),
// but without ever joining.
func TestServe_Say_BypassesHalt(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-agent"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpHalt, Room: "potato", Text: "stop"}); !resp.OK {
		t.Fatalf("halt: %s", resp.Error)
	}

	agentSend := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-agent", Text: "still going"})
	if agentSend.OK {
		t.Fatal("expected agent send into a halted room to fail")
	}

	sayResp := dialAndDo(t, addr, Request{Op: OpSay, Room: "potato", Name: "human", Kind: "human", Text: "stop right there"})
	if !sayResp.OK {
		t.Fatalf("expected say to bypass halt, got: %s", sayResp.Error)
	}
}

// --- misc dispatch ---

func TestServe_UnknownOp_ReturnsUsageError(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)

	resp := dialAndDo(t, ln.Addr().String(), Request{Op: "not-a-real-op"})
	if resp.OK {
		t.Fatal("expected an unknown op to fail")
	}
	if resp.Code != ExitUsage {
		t.Fatalf("Code = %d, want ExitUsage (%d)", resp.Code, ExitUsage)
	}
}

// --- shutdown op ---

func TestServe_ShutdownOp_ReturnsOKAndStopsTheDaemon(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, nil)
	}()

	resp := dialAndDo(t, ln.Addr().String(), Request{Op: OpShutdown})
	if !resp.OK {
		t.Fatalf("shutdown failed: %s", resp.Error)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned an error after shutdown: %v", err)
		}
	case <-time.After(wireTimeout):
		t.Fatal("Serve did not return after the shutdown op")
	}
}

// TestServe_NoTimerStopsTheDaemon proves the daemon has no idle-shutdown
// timer: with no subscriptions, no connections, and no activity at all, it
// must still be running well past what used to be the default idle window.
// Only OpShutdown (proven above) or ctx cancellation ever ends Serve.
func TestServe_NoTimerStopsTheDaemon(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, nil)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(wireTimeout):
			t.Error("Serve did not exit within the bounded wait after cancellation")
		}
	})

	select {
	case err := <-done:
		t.Fatalf("Serve shut down on its own with no activity and no timer configured (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
		// expected: still running
	}
}

// TestServe_Say_IgnoresClientSuppliedIdentity is the wire-level regression test
// for the impersonation hole a reviewer proved by speaking the socket directly.
//
// The daemon used to forward req.Name and req.Kind to the publish path, so a
// raw OpSay claiming an existing agent's name with kind "agent" published under
// that identity — and, because the operator path does not consult the halt
// flag, did so into a halted room. Pinning the identity in the CLI wrapper was
// no defense: the socket is the trust boundary, and any local process can
// speak it.
//
// This asserts the daemon overrides a hostile claim rather than merely that the
// honest client works.
func TestServe_Say_IgnoresClientSuppliedIdentity(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub)
	addr := ln.Addr().String()

	joinResp := dialAndDo(t, addr, Request{
		Op: OpJoin, Room: "potato", Name: "backend", Mode: "participate",
		Kind: KindAgent, Session: "sess-agent",
	})
	if !joinResp.OK {
		t.Fatalf("join: %s", joinResp.Error)
	}
	if haltResp := dialAndDo(t, addr, Request{Op: OpHalt, Room: "potato", Text: "stop"}); !haltResp.OK {
		t.Fatalf("halt: %s", haltResp.Error)
	}

	// A hostile request: claim to be the joined agent, in a halted room.
	resp := dialAndDo(t, addr, Request{
		Op: OpSay, Room: "potato", Name: "backend", Kind: KindAgent, Text: "forged",
	})
	if !resp.OK {
		t.Fatalf("say: %s", resp.Error)
	}

	var payload struct {
		Envelope Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("decode say payload: %v", err)
	}
	if payload.Envelope.From != operatorName {
		t.Errorf("From = %q, want %q: the daemon honored a client-supplied name", payload.Envelope.From, operatorName)
	}
	if payload.Envelope.FromKind != KindHuman {
		t.Errorf("FromKind = %q, want %q: the daemon honored a client-supplied kind", payload.Envelope.FromKind, KindHuman)
	}
}
