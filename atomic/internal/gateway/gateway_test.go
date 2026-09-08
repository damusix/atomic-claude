package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

const wireTimeout = 5 * time.Second

// testUnixListener binds a unix socket for one test under /tmp rather than
// t.TempDir(), which honors $TMPDIR and can exceed the ~104-byte unix socket
// path limit — same reasoning as internal/bus's own testListener.
func testUnixListener(t *testing.T) net.Listener {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atomicgw")
	if err != nil {
		dir = t.TempDir()
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "d.sock"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// startDaemon runs bus.Serve directly on unixLn — never spawnServe, which
// under `go test` would re-run the whole suite — and guarantees it exits
// before the test ends.
func startDaemon(t *testing.T, unixLn net.Listener, home string) {
	t.Helper()
	hub := bus.NewHub(home)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Serve(ctx, unixLn, hub, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(wireTimeout):
			t.Error("daemon did not exit within the bounded wait")
		}
	})
}

// newTestServer wires a Server against a freshly started daemon and returns
// it plus its key Store, ready to enroll callers.
func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	unixLn := testUnixListener(t)
	home := t.TempDir()
	startDaemon(t, unixLn, home)

	store := NewStore(filepath.Join(t.TempDir(), "keys.json"))
	window := NewWindow(AdmissionWindow, time.Now())
	addr := unixLn.Addr().String()

	return &Server{
		Store:  store,
		Window: window,
		dial:   func() (net.Conn, error) { return net.Dial("unix", addr) },
	}, store
}

func sealedRequest(t *testing.T, key []byte, keyID string, req bus.Request, now time.Time) ([]byte, [12]byte) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	frame, nonce, err := remote.SealC2S(key, []byte(keyID), body, now)
	if err != nil {
		t.Fatalf("SealC2S: %v", err)
	}
	return frame, nonce
}

// readFrame reads one length-prefixed frame off r, the framing gateway.go
// writes between sealed lines on a streaming response.
func readFrame(r io.Reader) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	buf := make([]byte, binary.BigEndian.Uint32(length[:]))
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func openS2C(t *testing.T, key []byte, nonce [12]byte, frame []byte, wantSeq uint64) bus.Response {
	t.Helper()
	plaintext, _, err := remote.OpenS2C(key, nonce, frame, wantSeq)
	if err != nil {
		t.Fatalf("OpenS2C(seq=%d): %v", wantSeq, err)
	}
	var resp bus.Response
	if err := json.Unmarshal(plaintext, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

func postFrame(t *testing.T, url string, frame []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

// TestHandleOp_OneShotAndStream_SameCodePath proves a one-shot ping and a
// recv subscription both come back through copyStream — the
// ping's single sealed frame is followed by EOF, and recv's first frame is
// the same {"ok":true} shape before further frames continue to arrive.
func TestHandleOp_OneShotAndStream_SameCodePath(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	// One-shot: ping.
	pingFrame, pingNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpPing}, now)
	pingResp := postFrame(t, ts.URL+"/v1/op", pingFrame)
	defer pingResp.Body.Close()

	frame0, err := readFrame(pingResp.Body)
	if err != nil {
		t.Fatalf("read ping frame: %v", err)
	}
	if resp := openS2C(t, rec.Key, pingNonce, frame0, 0); !resp.OK {
		t.Fatalf("ping response not ok: %+v", resp)
	}
	if _, err := readFrame(pingResp.Body); err != io.EOF {
		t.Fatalf("one-shot must end after its single frame, got err=%v", err)
	}

	// Streaming: recv, same handler, same framing, more than one frame.
	recvFrame, recvNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "potato"}, now)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(recvFrame))
	recvResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST recv: %v", err)
	}
	defer recvResp.Body.Close()

	subFrame, err := readFrame(recvResp.Body)
	if err != nil {
		t.Fatalf("read subscribe frame: %v", err)
	}
	if resp := openS2C(t, rec.Key, recvNonce, subFrame, 0); !resp.OK {
		t.Fatalf("subscribe response not ok: %+v", resp)
	}

	publishOnRoom(t, srv, "potato", "hello")

	envFrame, err := readFrame(recvResp.Body)
	if err != nil {
		t.Fatalf("read envelope frame: %v", err)
	}
	if resp := openS2C(t, rec.Key, recvNonce, envFrame, 1); resp.OK {
		// The envelope line is a bus.Envelope, not a bus.Response — decoding
		// it into Response leaves OK false with no error, which is expected;
		// this call only proves the frame opened and sequence 1 was correct.
	}
}

// publishOnRoom dials the daemon directly (bypassing the gateway) and
// publishes as the operator, which needs no prior membership.
func publishOnRoom(t *testing.T, srv *Server, room, text string) {
	t.Helper()
	conn, err := srv.dial()
	if err != nil {
		t.Fatalf("dial daemon: %v", err)
	}
	defer conn.Close()

	enc := json.NewEncoder(conn)
	if err := enc.Encode(bus.Request{Op: bus.OpSay, Room: room, Text: text}); err != nil {
		t.Fatalf("encode say: %v", err)
	}
	dec := json.NewDecoder(conn)
	var resp bus.Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode say response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("say failed: %+v", resp)
	}
}

// TestAdmit_TwoSessionsOneKey_AppearAsTwoMembers proves end to end over HTTP
// that two joins under one key with
// different client sessions must be two distinct roster members.
func TestGateway_TwoSessionsOneKey_AppearAsTwoMembers(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("laptop")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	for _, session := range []string{"session-a", "session-b"} {
		frame, nonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{
			Op: bus.OpJoin, Room: "potato", Session: session, Name: session, Mode: "agent", Kind: "agent",
		}, now)
		resp := postFrame(t, ts.URL+"/v1/op", frame)
		f, err := readFrame(resp.Body)
		if err != nil {
			t.Fatalf("read join frame: %v", err)
		}
		resp.Body.Close()
		if r := openS2C(t, rec.Key, nonce, f, 0); !r.OK {
			t.Fatalf("join %q failed: %+v", session, r)
		}
	}

	whoFrame, whoNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpWho, Room: "potato"}, now)
	whoResp := postFrame(t, ts.URL+"/v1/op", whoFrame)
	defer whoResp.Body.Close()
	f, err := readFrame(whoResp.Body)
	if err != nil {
		t.Fatalf("read who frame: %v", err)
	}
	r := openS2C(t, rec.Key, whoNonce, f, 0)
	if !r.OK {
		t.Fatalf("who failed: %+v", r)
	}
	var payload struct {
		Members []struct {
			Name string `json:"name"`
		} `json:"members"`
	}
	if err := json.Unmarshal(r.Payload, &payload); err != nil {
		t.Fatalf("unmarshal who payload: %v", err)
	}
	if len(payload.Members) != 2 {
		t.Fatalf("expected 2 members from one key with two sessions, got %d: %+v", len(payload.Members), payload.Members)
	}
	if payload.Members[0].Name == payload.Members[1].Name {
		t.Fatalf("two sessions under one key produced one member name %q", payload.Members[0].Name)
	}
}

// TestGateway_AdmissionDrop_DoesNotEndConcurrentStream proves a dropped frame
// on one connection must not take down a live recv stream on
// another connection from the same client — the reason h2 is disabled.
func TestGateway_AdmissionDrop_DoesNotEndConcurrentStream(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	recvFrame, recvNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "potato"}, now)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(recvFrame))
	recvResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST recv: %v", err)
	}
	defer recvResp.Body.Close()
	if _, err := readFrame(recvResp.Body); err != nil {
		t.Fatalf("read subscribe frame: %v", err)
	}

	// A tampered frame on a fresh connection must be silently dropped.
	badFrame := append([]byte(nil), recvFrame...)
	badFrame[len(badFrame)-1] ^= 0xFF
	dropResp, err := http.Post(ts.URL+"/v1/op", "application/octet-stream", bytes.NewReader(badFrame))
	if err == nil {
		dropResp.Body.Close()
		t.Fatalf("expected the admission drop to close the connection with no response, got a response")
	}

	publishOnRoom(t, srv, "potato", "still alive")

	envFrame, err := readFrame(recvResp.Body)
	if err != nil {
		t.Fatalf("recv stream did not survive the concurrent admission drop: %v", err)
	}
	if _, _, err := remote.OpenS2C(rec.Key, recvNonce, envFrame, 1); err != nil {
		t.Fatalf("OpenS2C after the drop: %v", err)
	}
}

// TestGateway_Revoke_EndsLiveStreamWithinOneFrame proves that once a key is
// revoked, the next line the stream would have sealed is instead the
// end of the stream, without a gateway restart or a registry of streams.
func TestGateway_Revoke_EndsLiveStreamWithinOneFrame(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	recvFrame, _ := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "potato"}, now)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(recvFrame))
	recvResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST recv: %v", err)
	}
	defer recvResp.Body.Close()
	if _, err := readFrame(recvResp.Body); err != nil {
		t.Fatalf("read subscribe frame: %v", err)
	}

	if err := store.Revoke("web-api"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	publishOnRoom(t, srv, "potato", "after revoke")

	if _, err := readFrame(recvResp.Body); err == nil {
		t.Fatalf("expected the stream to end after revocation, got another frame")
	}
}

// TestGateway_SlowReader_DoesNotStallOtherSubscribers proves a subscriber that
// never reads its response body must not delay a second
// subscriber on a different room, since each request runs its own goroutine
// and its own daemon connection.
func TestGateway_SlowReader_DoesNotStallOtherSubscribers(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	slowFrame, _ := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "slow-room"}, now)
	slowReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(slowFrame))
	slowResp, err := http.DefaultClient.Do(slowReq)
	if err != nil {
		t.Fatalf("POST slow recv: %v", err)
	}
	t.Cleanup(func() { slowResp.Body.Close() })
	// Deliberately never read slowResp.Body further.

	fastFrame, fastNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "fast-room"}, now)
	fastReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(fastFrame))
	fastResp, err := http.DefaultClient.Do(fastReq)
	if err != nil {
		t.Fatalf("POST fast recv: %v", err)
	}
	defer fastResp.Body.Close()
	if _, err := readFrame(fastResp.Body); err != nil {
		t.Fatalf("read fast subscribe frame: %v", err)
	}

	publishOnRoom(t, srv, "fast-room", "not stalled")

	done := make(chan error, 1)
	go func() {
		_, err := readFrame(fastResp.Body)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("fast subscriber did not receive its envelope: %v", err)
		}
	case <-time.After(wireTimeout):
		t.Fatalf("fast subscriber stalled behind the slow reader's unread stream")
	}
	_ = fastNonce
}

// TestGateway_OversizedFrame_RejectedBeforeDial proves a body over
// MaxFrameBytes is rejected before the gateway ever dials the daemon.
func TestGateway_OversizedFrame_RejectedBeforeDial(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "keys.json"))
	window := NewWindow(AdmissionWindow, time.Now())

	var dialed bool
	srv := &Server{
		Store:  store,
		Window: window,
		dial: func() (net.Conn, error) {
			dialed = true
			return nil, io.EOF
		},
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	oversized := bytes.Repeat([]byte{0}, MaxFrameBytes+1)
	resp, err := http.Post(ts.URL+"/v1/op", "application/octet-stream", bytes.NewReader(oversized))
	if err == nil {
		resp.Body.Close()
		t.Fatalf("expected the oversized frame to close the connection with no response")
	}
	if dialed {
		t.Fatalf("gateway dialled the daemon for a frame over the cap")
	}

	// Contrast: a small, merely-malformed frame does reach the dial step.
	small := []byte{1, 2, 3}
	resp2, err := http.Post(ts.URL+"/v1/op", "application/octet-stream", bytes.NewReader(small))
	if err == nil {
		resp2.Body.Close()
	}
	// A malformed header never parses far enough to reach Lookup/dial, so
	// this only proves the cap path specifically skips dial, not that every
	// drop path does.
}

// TestGateway_SendAtLocalTextCap_NotRejectedForSize proves finding 7 of the
// final review: MaxFrameBytes must carry headroom over bus.MaxTextBytes, not
// equal it. A say whose Text sits at the local cap grows past MaxTextBytes
// once JSON-escaped into a Request and wrapped in the sealed frame's header
// and AEAD tag; with no headroom that legal-locally send was silently
// dropped as "unreachable" the moment it crossed the gateway.
func TestGateway_SendAtLocalTextCap_NotRejectedForSize(t *testing.T) {
	srv, store := newTestServer(t)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	req := bus.Request{Op: bus.OpSay, Room: "potato", Text: strings.Repeat("x", bus.MaxTextBytes)}
	frame, nonce := sealedRequest(t, rec.Key, rec.ID, req, time.Now())
	if len(frame) <= 1<<20 {
		t.Fatalf("test frame is %d bytes, want it to exceed the old 1<<20 cap to actually exercise the headroom", len(frame))
	}
	if len(frame) > MaxFrameBytes {
		t.Fatalf("test frame is %d bytes, over MaxFrameBytes (%d) — the headroom is too small for a Text at bus.MaxTextBytes", len(frame), MaxFrameBytes)
	}

	resp := postFrame(t, ts.URL+"/v1/op", frame)
	defer resp.Body.Close()

	// Reaching a decodable response at all — OK or not — proves the gateway
	// read the whole body and dialed the daemon rather than dropping it for
	// size, which the old cap did silently.
	respFrame, err := readFrame(resp.Body)
	if err != nil {
		t.Fatalf("read response frame: %v (a size-based drop closes with no bytes at all)", err)
	}
	_ = openS2C(t, rec.Key, nonce, respFrame, 0)
}

// TestGateway_ResponseSequence_MonotonicFromZero_HeartbeatsIncluded proves
// the sequence starts at zero and heartbeats advance it exactly like real
// daemon output does.
func TestGateway_ResponseSequence_MonotonicFromZero_HeartbeatsIncluded(t *testing.T) {
	srv, store := newTestServer(t)
	srv.Heartbeat = 30 * time.Millisecond
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	now := time.Now()

	recvFrame, recvNonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpRecv, Room: "potato"}, now)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/op", bytes.NewReader(recvFrame))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST recv: %v", err)
	}
	defer resp.Body.Close()

	for seq := uint64(0); seq < 3; seq++ {
		frame, err := readFrame(resp.Body)
		if err != nil {
			t.Fatalf("read frame seq=%d: %v", seq, err)
		}
		plaintext, _, err := remote.OpenS2C(rec.Key, recvNonce, frame, seq)
		if err != nil {
			t.Fatalf("OpenS2C seq=%d: %v", seq, err)
		}
		if seq >= 1 && !bytes.Equal(plaintext, heartbeatPlaintext) {
			t.Fatalf("seq=%d: expected a heartbeat frame, got %s", seq, plaintext)
		}
	}
}

// fakeHijacker wraps a ResponseWriter that satisfies http.Hijacker but
// always fails to hijack, for exercising hijackClose's failure path.
type fakeHijacker struct {
	http.ResponseWriter
}

func (fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("hijack: boom")
}

// TestHijackClose_NotHijacker_Panics proves finding 2: a response writer
// that does not support Hijack must not fall through to net/http's implicit
// 200, since that is a distinguishable outcome from the silent drop
// hijackClose exists to produce. h2 is disabled, so this is unreachable in
// normal operation; the panic makes it unreachable by construction instead.
func TestHijackClose_NotHijacker_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("hijackClose on a non-Hijacker must panic rather than fall through")
		}
	}()
	hijackClose(httptest.NewRecorder())
}

// TestHijackClose_HijackFails_Panics covers the other silent-fallthrough
// path: Hijack itself returning an error.
func TestHijackClose_HijackFails_Panics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("hijackClose on a failed Hijack must panic rather than fall through")
		}
	}()
	hijackClose(fakeHijacker{httptest.NewRecorder()})
}

func TestNewHTTPServer_DisablesH2(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())
	if srv.TLSNextProto == nil || len(srv.TLSNextProto) != 0 {
		t.Fatalf("TLSNextProto must be a cleared, empty map to disable h2, got %v", srv.TLSNextProto)
	}
}

// TestRun_ServesOverHTTPAndStartsDaemon is the smoke test for Run itself:
// bus.Serve driven directly (never spawnServe), a plain HTTP listener, and a
// round trip through the resulting /v1/op endpoint.
func TestRun_ServesOverHTTPAndStartsDaemon(t *testing.T) {
	unixLn := testUnixListener(t)
	home := t.TempDir()

	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}

	store := NewStore(filepath.Join(t.TempDir(), "keys.json"))
	window := NewWindow(AdmissionWindow, time.Now())
	rec, err := store.Enroll("web-api")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- Run(ctx, home, unixLn, httpLn, "", "", store, window, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-runDone:
		case <-time.After(wireTimeout):
			t.Error("Run did not exit within the bounded wait")
		}
	})

	url := "http://" + httpLn.Addr().String() + "/v1/op"
	now := time.Now()
	frame, nonce := sealedRequest(t, rec.Key, rec.ID, bus.Request{Op: bus.OpPing}, now)

	var resp *http.Response
	for i := 0; i < 20; i++ {
		resp, err = http.Post(url, "application/octet-stream", bytes.NewReader(frame))
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	f, err := readFrame(resp.Body)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	r := openS2C(t, rec.Key, nonce, f, 0)
	if !r.OK {
		t.Fatalf("ping via Run failed: %+v", r)
	}
}

// TestGateway_CopyStream_ReaderGoroutineExitsOnClientHangUp proves the
// reader goroutine does not leak when the main loop returns (here: a client
// hang-up via context cancellation) while a line is still pending. Before the
// fix, the reader's send select could only be unblocked by readDone, which
// only its own deferred close ever closes — a channel a goroutine blocked
// inside its own select cannot itself cause to fire — so a line arriving
// after the main loop exits would block that goroutine forever.
func TestGateway_CopyStream_ReaderGoroutineExitsOnClientHangUp(t *testing.T) {
	srv, _ := newTestServer(t)

	daemonSide, gatewaySide := net.Pipe()
	t.Cleanup(func() {
		daemonSide.Close()
		gatewaySide.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/op", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	mainLoopDone := make(chan struct{})
	go func() {
		srv.copyStream(w, req, gatewaySide, "web-api", [12]byte{})
		close(mainLoopDone)
	}()

	// Cancel before the daemon side ever writes anything, so the main loop's
	// first select has only ctx.Done() ready and returns immediately, with
	// the reader goroutine still parked in ReadBytes.
	cancel()
	select {
	case <-mainLoopDone:
	case <-time.After(wireTimeout):
		t.Fatal("copyStream did not return after the request context was cancelled")
	}

	// Hand the still-running reader a line now that the main loop is gone.
	// It reads it and tries to hand it off — the case this test exists for.
	writeErr := make(chan error, 1)
	go func() {
		_, err := daemonSide.Write([]byte("late line\n"))
		writeErr <- err
	}()
	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("write to daemon side: %v", err)
		}
	case <-time.After(wireTimeout):
		t.Fatal("write to the reader goroutine never unblocked")
	}

	// runtime.NumGoroutine alone is too noisy in this environment (other
	// goroutines churn independently), so this looks for the reader's own
	// frame by name in a full stack dump instead — a direct, unambiguous
	// check that copyStream's reader closure is no longer running.
	deadline := time.After(wireTimeout)
	for {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		stacks := string(buf[:n])
		if !strings.Contains(stacks, "gateway.(*Server).copyStream") {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("reader goroutine leaked: still running after being handed a line post-return\nSTACKS:\n%s", stacks)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
