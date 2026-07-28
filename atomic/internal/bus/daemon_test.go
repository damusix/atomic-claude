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
func startServe(t *testing.T, ln net.Listener, hub *Hub, idleWindow time.Duration) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, idleWindow, nil)
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
	startServe(t, ln, hub, 0)

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
	startServe(t, ln, hub, 0)
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
	want := RoomInfo{Name: "potato", Members: 0}
	if len(roomsPayload.Rooms) != 1 || roomsPayload.Rooms[0] != want {
		t.Fatalf("rooms = %+v, want [%+v] (room persists after everyone leaves, with a member count)", roomsPayload.Rooms, want)
	}
}

// TestServe_DuplicateNameRejected drives the wire-level dispatch path for
// the same atomic-claim guarantee room_test.go proves at the Hub level —
// this catches a bug in handleJoin's request decoding/response encoding
// that a pure Hub-level test could not see.
func TestServe_DuplicateNameRejected(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
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
	startServe(t, ln, hub, 0)
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

// TestServe_RecvFollow_DeliversPublishedEnvelope is the "under one second"
// success criterion end to end over the wire: subscribe with recv
// --follow, publish from a second connection, and read the delivered
// frame with a bounded timeout.
func TestServe_RecvFollow_DeliversPublishedEnvelope(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: "agent", Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join frontend: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "backend", Kind: "agent", Session: "sess-be"}); !resp.OK {
		t.Fatalf("join backend: %s", resp.Error)
	}

	subConn, r := dialSubscribe(t, addr, Request{Op: OpRecv, Room: "potato", Follow: true})
	defer subConn.Close()

	sendResp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", To: []string{"backend"}, Text: "please pick this up"})
	if !sendResp.OK {
		t.Fatalf("send failed: %s", sendResp.Error)
	}

	env, ok := readEnvelopeBounded(t, r)
	if !ok {
		t.Fatal("timed out waiting for the published envelope on recv --follow")
	}
	if env.Text != "please pick this up" {
		t.Fatalf("delivered Text = %q, want %q", env.Text, "please pick this up")
	}
	if env.From != "frontend" || len(env.To) != 1 || env.To[0] != "backend" {
		t.Fatalf("delivered envelope = %+v, want From=frontend To=[backend]", env)
	}
}

func TestServe_RecvOnce_WithoutFollowReturnsBacklogAndCloses(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
	addr := ln.Addr().String()

	if resp := dialAndDo(t, addr, Request{Op: OpJoin, Room: "potato", Name: "frontend", Kind: "agent", Session: "sess-fe"}); !resp.OK {
		t.Fatalf("join: %s", resp.Error)
	}
	if resp := dialAndDo(t, addr, Request{Op: OpSend, Room: "potato", Session: "sess-fe", Text: "one"}); !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	resp := dialAndDo(t, addr, Request{Op: OpRecv, Room: "potato"})
	if !resp.OK {
		t.Fatalf("recv failed: %s", resp.Error)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Envelopes) != 1 || payload.Envelopes[0].Text != "one" {
		t.Fatalf("envelopes = %+v, want one envelope with Text=one", payload.Envelopes)
	}
}

// TestServe_Tail_NeverJoinsAndSeesOthersMail proves the wire-level shape
// of docs/design/atomic-bus.md decision #5: tail does not join (no
// roster entry, confirmed via who) yet still receives traffic addressed
// to other members.
func TestServe_Tail_NeverJoinsAndSeesOthersMail(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
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

// --- halt / resume over the wire ---

func TestServe_Halt_BlocksAgentSend_HumanSendStillSucceeds_ResumeRestores(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	startServe(t, ln, hub, 0)
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
	startServe(t, ln, hub, 0)
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
	startServe(t, ln, hub, 0)
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
	startServe(t, ln, hub, 0)

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
		done <- Serve(ctx, ln, hub, 0, nil)
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

// --- idle shutdown ---

// TestServe_IdleShutdown_FiresAfterWindowWithNoSubscriptions uses a short
// injected window instead of DefaultIdleWindow's 10 minutes — the "make
// the window injectable" requirement exists exactly so this test doesn't
// sleep for ten minutes.
func TestServe_IdleShutdown_FiresAfterWindowWithNoSubscriptions(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, 30*time.Millisecond, nil)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned an error on idle shutdown: %v", err)
		}
	case <-time.After(wireTimeout):
		t.Fatal("Serve did not shut down after the idle window elapsed with no subscriptions")
	}
}

// TestServe_IdleShutdown_DisarmsWhileASubscriptionIsOpen proves a live
// recv --follow prevents the idle timer from firing even past the
// configured window, and that closing the subscription re-arms a fresh
// window that then fires normally.
func TestServe_IdleShutdown_DisarmsWhileASubscriptionIsOpen(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, 30*time.Millisecond, nil)
	}()

	subConn, _ := dialSubscribe(t, addr, Request{Op: OpTail, Rooms: []string{"potato"}})

	// Outlive several idle windows while the subscription is open; Serve
	// must not have shut down.
	select {
	case err := <-done:
		t.Fatalf("Serve shut down while a subscription was still open (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
	}

	// Closing the subscription should let a fresh idle window elapse and
	// shut the daemon down.
	subConn.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned an error on idle shutdown: %v", err)
		}
	case <-time.After(wireTimeout):
		t.Fatal("Serve did not shut down after the subscription closed and a fresh idle window elapsed")
	}
}

// TestServe_IdleShutdown_DoesNotFireWhileAConnectionIsMidAccept reproduces
// the checkpoint 2 review finding: the idle-fire handler only re-checked
// d.subs, so a connection accepted but not yet through
// subscriptionOpened/pendingResolved was invisible to it — the daemon
// could close the listener and return while a client's request was still
// in flight, orphaning that handleConn goroutine. This test dials without
// ever writing a request, so the server's Decode blocks — holding the
// connection "accepted but not yet classified" across several idle
// windows — then finally sends the request and proves the daemon is
// still alive to answer both it and a fresh connection afterward.
func TestServe_IdleShutdown_DoesNotFireWhileAConnectionIsMidAccept(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, 30*time.Millisecond, nil)
	}()

	conn, err := net.DialTimeout("unix", addr, wireTimeout)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-done:
		t.Fatalf("Serve shut down while a connection was accepted but not yet classified (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
	}

	// Finish what the held connection started.
	if err := json.NewEncoder(conn).Encode(Request{Op: OpPing}); err != nil {
		t.Fatalf("encode request on the previously-held connection: %v", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response on the previously-held connection: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ping on the previously-held connection failed: %s", resp.Error)
	}

	// The daemon must still be answering afterward — proves it neither
	// shut down nor got left permanently disarmed by the race.
	if resp := dialAndDo(t, addr, Request{Op: OpPing}); !resp.OK {
		t.Fatalf("ping after the held connection completed failed: %s", resp.Error)
	}
}

// TestServe_IdleWindowZero_DisablesIdleShutdown proves the documented
// "0 disables" contract: with no subscriptions and no activity at all,
// Serve must still be running after several multiples of what would
// otherwise have been an idle window.
func TestServe_IdleWindowZero_DisablesIdleShutdown(t *testing.T) {
	ln := testListener(t)
	hub := NewHub(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, ln, hub, 0, nil)
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
		t.Fatalf("Serve shut down with idleWindow=0, which must disable idle shutdown (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
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
	startServe(t, ln, hub, 0)
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
