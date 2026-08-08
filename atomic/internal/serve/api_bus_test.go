package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
)

// busTestHome creates a short-pathed home dir — the daemon socket lives at
// <home>/.atomic/bus.sock, and t.TempDir() can exceed the ~104-byte unix
// socket path limit on macOS (same rationale as bus's own testListener).
func busTestHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atomicbusweb")
	if err != nil {
		dir = t.TempDir()
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}
	return dir
}

// startBusDaemon runs an in-process bus daemon on home's socket and
// guarantees it exits before the test finishes.
func startBusDaemon(t *testing.T, home string) {
	t.Helper()
	if err := bus.EnsureDirs(home); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	ln, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- bus.Serve(ctx, ln, bus.NewHub(home), nil)
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("bus daemon did not exit after cancellation")
		}
	})
}

// newBusTestHandler builds the handler under test with the spawn seam
// replaced by a Dial-only variant, so no test can ever fork a real daemon.
func newBusTestHandler(home, targetDir string) http.Handler {
	return NewAPIBusHandler(BusAPIOptions{
		Home:      home,
		TargetDir: targetDir,
		EnsureDaemon: func(h string) (*bus.Client, error) {
			return bus.Dial(h, time.Second)
		},
	})
}

func postBusJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(url, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func decodeBusResponse[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

// TestAPIBus_LoopbackGate drives the handler's ServeHTTP directly with
// crafted RemoteAddr values (not real TCP connections) so a non-loopback
// peer can be asserted without binding a LAN-reachable listener. Covers a
// read route and a write route — the gate must run before either dispatches
// to the daemon.
func TestAPIBus_LoopbackGate(t *testing.T) {
	home := busTestHome(t)
	handler := newBusTestHandler(home, t.TempDir())

	tests := []struct {
		name       string
		remoteAddr string
		method     string
		path       string
		wantStatus int
	}{
		{"LAN peer, read route", "192.168.1.50:4242", http.MethodGet, "/api/bus/status", http.StatusForbidden},
		{"LAN peer, write route", "192.168.1.50:4242", http.MethodPost, "/api/bus/send", http.StatusForbidden},
		{"loopback IPv4", "127.0.0.1:5555", http.MethodGet, "/api/bus/status", http.StatusOK},
		{"loopback IPv6", "[::1]:5555", http.MethodGet, "/api/bus/status", http.StatusOK},
		{"empty RemoteAddr", "", http.MethodGet, "/api/bus/status", http.StatusForbidden},
		{"garbage RemoteAddr", "not-an-address", http.MethodGet, "/api/bus/status", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.RemoteAddr = tt.remoteAddr
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestAPIBus_StatusAndRooms_NoDaemon(t *testing.T) {
	home := busTestHome(t)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/bus/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	status := decodeBusResponse[busStatusResponse](t, resp)
	if status.Running {
		t.Error("status.running = true with no daemon")
	}
	if status.Name == "" {
		t.Error("status.name empty — position-derived identity should resolve without a daemon")
	}

	resp, err = http.Get(srv.URL + "/api/bus/rooms")
	if err != nil {
		t.Fatalf("GET rooms: %v", err)
	}
	rooms := decodeBusResponse[busRoomsResponse](t, resp)
	if rooms.Running || len(rooms.Rooms) != 0 {
		t.Errorf("rooms with no daemon = %+v, want running:false with empty list", rooms)
	}
}

func TestAPIBus_LogBackfill_TailsLastN(t *testing.T) {
	home := busTestHome(t)
	for i := 1; i <= 3; i++ {
		env := bus.Envelope{ID: fmt.Sprintf("m-%d", i), Room: "exp", From: "a", FromKind: "agent", Ts: time.Unix(int64(1000+i), 0), Text: fmt.Sprintf("msg %d", i)}
		if err := bus.Append(home, "exp", env); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/bus/log?room=exp&n=2")
	if err != nil {
		t.Fatalf("GET log: %v", err)
	}
	log := decodeBusResponse[busLogResponse](t, resp)
	if len(log.Envelopes) != 2 {
		t.Fatalf("got %d envelopes, want 2", len(log.Envelopes))
	}
	if log.Envelopes[0].ID != "m-2" || log.Envelopes[1].ID != "m-3" {
		t.Errorf("tail order = %s,%s, want m-2,m-3", log.Envelopes[0].ID, log.Envelopes[1].ID)
	}

	// A room with no log is empty history, not an error.
	resp, err = http.Get(srv.URL + "/api/bus/log?room=nolog")
	if err != nil {
		t.Fatalf("GET log nolog: %v", err)
	}
	empty := decodeBusResponse[busLogResponse](t, resp)
	if len(empty.Envelopes) != 0 {
		t.Errorf("missing log returned %d envelopes, want 0", len(empty.Envelopes))
	}
}

// TestAPIBus_JoinSendTailWho_EndToEnd drives the full chat flow against an
// in-process daemon: join creates the room, who lists the human member,
// tail streams a subsequently sent message, and halt does not block the
// human member's send (halt binds agents only).
func TestAPIBus_JoinSendTailWho_EndToEnd(t *testing.T) {
	home := busTestHome(t)
	startBusDaemon(t, home)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	// Join — creates the room ("open a channel if need be").
	resp := postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "exp"})
	joined := decodeBusResponse[map[string]string](t, resp)
	if joined["name"] == "" {
		t.Fatal("join returned empty name")
	}

	// Who — the web member is on the roster as a human.
	resp, err := http.Get(srv.URL + "/api/bus/who?room=exp")
	if err != nil {
		t.Fatalf("GET who: %v", err)
	}
	who := decodeBusResponse[busWhoResponse](t, resp)
	if len(who.Members) != 1 || who.Members[0].Kind != bus.KindHuman {
		t.Fatalf("who = %+v, want one human member", who.Members)
	}

	// Tail — subscribe first (headers arriving means the daemon-side
	// subscription is live), then send, then read the streamed envelope.
	tailResp, err := http.Get(srv.URL + "/api/bus/tail?room=exp")
	if err != nil {
		t.Fatalf("GET tail: %v", err)
	}
	defer tailResp.Body.Close()
	if ct := tailResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("tail content-type = %q", ct)
	}

	resp = postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "exp", "text": "hello agents"})
	sent := decodeBusResponse[busSendResponse](t, resp)
	if sent.Envelope.ID == "" || sent.Envelope.Text != "hello agents" {
		t.Fatalf("send envelope = %+v", sent.Envelope)
	}

	envCh := make(chan bus.Envelope, 1)
	go func() {
		scanner := bufio.NewScanner(tailResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var env bus.Envelope
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env) == nil {
				envCh <- env
				return
			}
		}
	}()
	select {
	case env := <-envCh:
		if env.Text != "hello agents" || env.FromKind != bus.KindHuman {
			t.Errorf("tailed envelope = %+v", env)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tail did not deliver the sent envelope")
	}

	// Halt binds agents, not the joined human member — send still lands.
	resp = postBusJSON(t, srv.URL+"/api/bus/halt", map[string]string{"room": "exp", "reason": "taking the wheel"})
	resp.Body.Close()
	resp = postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "exp", "text": "still here"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("human send into halted room = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()
	resp = postBusJSON(t, srv.URL+"/api/bus/resume", map[string]string{"room": "exp"})
	resp.Body.Close()

	// Send without a prior join into a fresh room — the join-if-needed
	// path creates room + membership on the way.
	resp = postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "fresh", "text": "auto-join"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("auto-join send = %d, want 200", resp.StatusCode)
	}
	autoSent := decodeBusResponse[busSendResponse](t, resp)
	if autoSent.Name == "" {
		t.Error("auto-join send returned empty member name")
	}

	// Sessions — the web member is listed; no transcript file exists yet,
	// then one appears under <home>/.claude/projects and is found.
	resp, err = http.Get(srv.URL + "/api/bus/sessions?room=exp")
	if err != nil {
		t.Fatalf("GET sessions: %v", err)
	}
	sessions := decodeBusResponse[busSessionsResponse](t, resp)
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].Transcript.Found {
		t.Fatalf("sessions before transcript = %+v, want one member, transcript not found", sessions.Sessions)
	}
	writeFakeTranscript(t, home, sessions.Sessions[0].Session)
	resp, err = http.Get(srv.URL + "/api/bus/sessions?room=exp")
	if err != nil {
		t.Fatalf("GET sessions: %v", err)
	}
	sessions = decodeBusResponse[busSessionsResponse](t, resp)
	if !sessions.Sessions[0].Transcript.Found {
		t.Error("transcript not found after writing the .jsonl")
	}
}

// writeFakeTranscript writes a minimal Claude Code session .jsonl for
// session under home's projects dir.
func writeFakeTranscript(t *testing.T, home, session string) string {
	t.Helper()
	dir := home + "/.claude/projects/-tmp-demo"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	lines := []string{
		`{"type":"ai-title","aiTitle":"Fix the rounding bug"}`,
		`{"type":"user","timestamp":"2026-08-08T12:00:00Z","message":{"role":"user","content":"fix the cart rounding bug"}}`,
		`{"type":"assistant","timestamp":"2026-08-08T12:00:10Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"where does the total get rounded"},{"type":"text","text":"Found it — banker's rounding in **totals.ts:88**."},{"type":"tool_use","name":"Read","input":{"file_path":"totals.ts"}}]}}`,
		`{"type":"user","timestamp":"2026-08-08T12:00:12Z","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"export function total() { … }"}]}]}}`,
	}
	path := dir + "/" + session + ".jsonl"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func TestAPIBus_Transcript_RendersMarkdown(t *testing.T) {
	home := busTestHome(t)
	writeFakeTranscript(t, home, "sess-x")
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/bus/transcript?session=sess-x")
	if err != nil {
		t.Fatalf("GET transcript: %v", err)
	}
	tr := decodeBusResponse[busTranscriptResponse](t, resp)
	if tr.Title != "Fix the rounding bug" {
		t.Errorf("title = %q, want ai-title value", tr.Title)
	}
	if tr.TotalEntries != 3 || tr.ShownEntries != 3 {
		t.Errorf("entries = %d/%d, want 3/3", tr.ShownEntries, tr.TotalEntries)
	}
	for _, want := range []string{"<strong>totals.ts:88</strong>", "tool output", "Read", "thinking"} {
		if !strings.Contains(tr.HTML, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}

	// Offset pages backward from the tail: with 3 entries and n=1,
	// offset=1 lands on the middle entry (the assistant turn).
	resp, err = http.Get(srv.URL + "/api/bus/transcript?session=sess-x&n=1&offset=1")
	if err != nil {
		t.Fatalf("GET transcript offset: %v", err)
	}
	paged := decodeBusResponse[busTranscriptResponse](t, resp)
	if paged.ShownEntries != 1 || paged.Offset != 1 {
		t.Errorf("paged = %d shown, offset %d, want 1/1", paged.ShownEntries, paged.Offset)
	}
	if !strings.Contains(paged.HTML, "totals.ts:88") || strings.Contains(paged.HTML, "tool output") {
		t.Errorf("offset=1 window should be the assistant entry only, got: %.200s", paged.HTML)
	}
	if !strings.Contains(paged.HTML, "entries 2–2 of 3") {
		t.Errorf("range note missing, got: %.200s", paged.HTML)
	}

	// Unknown session → 404; path-shaped session → 400.
	resp, err = http.Get(srv.URL + "/api/bus/transcript?session=nope")
	if err != nil {
		t.Fatalf("GET transcript nope: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown session = %d, want 404", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/api/bus/transcript?session=..%2F..%2Fetc")
	if err != nil {
		t.Fatalf("GET transcript traversal: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("traversal session = %d, want 400", resp.StatusCode)
	}
}
