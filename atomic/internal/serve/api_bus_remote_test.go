// Remote-routing tests for busAPIHandler: a room tagged with a configured
// host must reach that host's gateway on all four paths (do, tail, log,
// rooms) instead of the local socket. Runs a real gateway.Server plus its
// own bus daemon on live listeners — gateway does not import serve, so this
// is a white-box test in package serve, unlike remote's own external test
// package (which exists only to dodge gateway importing bus/remote).
package serve

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/gateway"
)

// startTestGateway runs a real gateway.Server and its own bus daemon on live
// listeners. Mirrors internal/bus/remote/client_external_test.go's helper.
func startTestGateway(t *testing.T) (baseURL string, store *gateway.Store) {
	t.Helper()

	unixDir, err := os.MkdirTemp("/tmp", "atomicservegw")
	if err != nil {
		unixDir = t.TempDir()
	} else {
		t.Cleanup(func() { _ = os.RemoveAll(unixDir) })
	}
	unixLn, err := net.Listen("unix", filepath.Join(unixDir, "d.sock"))
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	httpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}

	store = gateway.NewStore(filepath.Join(t.TempDir(), "keys.json"))
	window := gateway.NewWindow(gateway.AdmissionWindow, time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, t.TempDir(), unixLn, httpLn, "", "", store, window, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("gateway did not stop within the bounded wait")
		}
	})

	return "http://" + httpLn.Addr().String(), store
}

// writeRemotesConfig writes the [bus.remotes.<name>] table a browser-facing
// home reads to resolve host to a gateway.
func writeRemotesConfig(t *testing.T, home, name, host string, key []byte) {
	t.Helper()
	dir := filepath.Dir(config.TOMLPath(home))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	toml := fmt.Sprintf("[bus.remotes.%s]\nhost = %q\nkey  = %q\n", name, host, hex.EncodeToString(key))
	if err := os.WriteFile(config.TOMLPath(home), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

// TestAPIBus_Join_RemoteHost_RoutesToGateway proves "do" per-room routing:
// join, then send and who, on a room tagged with a configured remote host
// reach the gateway rather than the (nonexistent) local socket.
func TestAPIBus_Join_RemoteHost_RoutesToGateway(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	resp := postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"})
	joined := decodeBusResponse[map[string]string](t, resp)
	if joined["name"] == "" {
		t.Fatalf("join over remote host returned empty name (resp status %d)", resp.StatusCode)
	}

	resp = postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "potato", "text": "hi from serve", "host": "prod"})
	sent := decodeBusResponse[busSendResponse](t, resp)
	if sent.Envelope.Text != "hi from serve" {
		t.Fatalf("send over remote host = %+v", sent)
	}

	resp, err = http.Get(srv.URL + "/api/bus/who?room=potato&host=prod")
	if err != nil {
		t.Fatalf("GET who: %v", err)
	}
	who := decodeBusResponse[busWhoResponse](t, resp)
	if len(who.Members) != 1 || who.Members[0].Kind != bus.KindHuman {
		t.Fatalf("who over remote host = %+v, want one human member", who.Members)
	}

	// No socket exists at home's own .atomic — a local-routed call would 503.
	if _, err := os.Stat(bus.SocketPath(home)); !os.IsNotExist(err) {
		t.Fatalf("expected no local socket to have been created, stat err = %v", err)
	}
}

// TestAPIBus_Tail_RemoteHost_StreamsOverGateway proves handleTail's remote
// path: a room tagged with a configured host streams the gateway's sealed
// tail, decoded to the same "data: <envelope json>\n\n" SSE shape the local
// path produces, with the subscribe handshake's own confirmation frame never
// forwarded as a fake envelope.
func TestAPIBus_Tail_RemoteHost_StreamsOverGateway(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	tailResp, err := http.Get(srv.URL + "/api/bus/tail?room=potato&host=prod")
	if err != nil {
		t.Fatalf("GET tail: %v", err)
	}
	defer tailResp.Body.Close()
	if ct := tailResp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("tail content-type = %q, want text/event-stream", ct)
	}

	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"}).Body.Close()
	postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "potato", "text": "over the wire", "host": "prod"}).Body.Close()

	envCh := make(chan bus.Envelope, 1)
	go func() {
		scanner := bufio.NewScanner(tailResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var env bus.Envelope
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env) == nil && env.Text != "" {
				envCh <- env
				return
			}
		}
	}()
	select {
	case env := <-envCh:
		if env.Text != "over the wire" {
			t.Errorf("tailed envelope = %+v", env)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("remote tail did not deliver the sent envelope")
	}
}

// TestAPIBus_Log_RemoteHost_EmptyByDesign proves handleLog's remote path: with
// no bulk-history wire op (OpTail is live-only, OpRead answers one id at a
// time, and nothing ever requests an id), a remote room's backlog is always
// empty rather than opening a room-log file that does not exist on this
// machine. The SSE tail fills the transcript live instead.
func TestAPIBus_Log_RemoteHost_EmptyByDesign(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"}).Body.Close()
	postBusJSON(t, srv.URL+"/api/bus/send", map[string]any{"room": "potato", "text": "recoverable", "host": "prod"}).Body.Close()

	resp, err := http.Get(srv.URL + "/api/bus/log?room=potato&host=prod")
	if err != nil {
		t.Fatalf("GET log: %v", err)
	}
	log := decodeBusResponse[busLogResponse](t, resp)
	if len(log.Envelopes) != 0 {
		t.Errorf("log over remote host = %d envelopes, want 0 (no bulk-history wire op)", len(log.Envelopes))
	}
}

// TestAPIBus_Rooms_FansOutAcrossRemotes proves handleRooms fans out: the
// local list (empty, no daemon here) plus every configured remote's rooms
// land in one list, each remote room tagged with its host.
func TestAPIBus_Rooms_FansOutAcrossRemotes(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"}).Body.Close()

	resp, err := http.Get(srv.URL + "/api/bus/rooms")
	if err != nil {
		t.Fatalf("GET rooms: %v", err)
	}
	rooms := decodeBusResponse[busRoomsResponse](t, resp)
	if len(rooms.Rooms) != 1 {
		t.Fatalf("rooms = %+v, want exactly the remote room", rooms.Rooms)
	}
	if rooms.Rooms[0].Name != "potato" || rooms.Rooms[0].Host != "prod" {
		t.Errorf("room entry = %+v, want name potato tagged host prod", rooms.Rooms[0])
	}
}

// TestAPIBus_Rooms_FanOutIsConcurrent_OneDeadRemoteDoesNotStackTheOthers
// proves finding 6 of the final review: handleRooms must fan out across
// [bus.remotes] concurrently, each bounded by h.dialTimeout, rather than one
// remote at a time — the frontend polls /api/bus/rooms every 4 seconds, and
// a sequential fan-out bounded only by remote.Client's general default made
// one unreachable remote stack every later poll behind it.
//
// "dead" never answers (its handler blocks until the client's own request
// context is cancelled by dialTimeout); "prod" is a real gateway. A
// sequential fan-out takes at least 2*dialTimeout; a concurrent one takes
// about 1*dialTimeout, however many remotes are configured.
func TestAPIBus_Rooms_FanOutIsConcurrent_OneDeadRemoteDoesNotStackTheOthers(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	unblock := make(chan struct{})
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-unblock:
		}
	}))
	// Registered in this order so Cleanup's LIFO unwind closes unblock
	// before Close: httptest.Server.Close waits for every in-flight handler
	// to return, and this handler only returns once the request context is
	// cancelled (the case under test) or unblock closes (the fallback).
	t.Cleanup(deadSrv.Close)
	t.Cleanup(func() { close(unblock) })

	home := busTestHome(t)
	dir := filepath.Dir(config.TOMLPath(home))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	toml := fmt.Sprintf(
		"[bus.remotes.dead]\nhost = %q\nkey  = %q\n\n[bus.remotes.prod]\nhost = %q\nkey  = %q\n",
		deadSrv.URL, hex.EncodeToString(make([]byte, 32)),
		baseURL, hex.EncodeToString(rec.Key),
	)
	if err := os.WriteFile(config.TOMLPath(home), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	const dialTimeout = 300 * time.Millisecond
	handler := NewAPIBusHandler(BusAPIOptions{
		Home:        home,
		TargetDir:   t.TempDir(),
		DialTimeout: dialTimeout,
		EnsureDaemon: func(h string) (*bus.Client, error) {
			return bus.Dial(h, time.Second)
		},
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"}).Body.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL + "/api/bus/rooms")
	if err != nil {
		t.Fatalf("GET rooms: %v", err)
	}
	elapsed := time.Since(start)
	rooms := decodeBusResponse[busRoomsResponse](t, resp)

	if len(rooms.Rooms) != 1 || rooms.Rooms[0].Host != "prod" {
		t.Fatalf("rooms = %+v, want exactly the prod room despite the dead remote", rooms.Rooms)
	}
	if elapsed >= 2*dialTimeout {
		t.Fatalf("GET rooms took %v against a %v dial timeout with 2 remotes — the dead one stacked ahead of prod instead of running concurrently", elapsed, dialTimeout)
	}
}

// TestAPIBus_TwoRoomsNamedPotato_StayDistinctByHost proves success criterion
// 2: a local "potato" and a remote "potato" are two distinct entries, told
// apart only by Host.
func TestAPIBus_TwoRoomsNamedPotato_StayDistinctByHost(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	startBusDaemon(t, home)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato"}).Body.Close()
	postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "potato", "host": "prod"}).Body.Close()

	resp, err := http.Get(srv.URL + "/api/bus/rooms")
	if err != nil {
		t.Fatalf("GET rooms: %v", err)
	}
	rooms := decodeBusResponse[busRoomsResponse](t, resp)
	if len(rooms.Rooms) != 2 {
		t.Fatalf("rooms = %+v, want a local and a remote potato", rooms.Rooms)
	}
	var sawLocal, sawRemote bool
	for _, r := range rooms.Rooms {
		if r.Name != "potato" {
			t.Errorf("unexpected room %+v", r)
			continue
		}
		if r.Host == "" {
			sawLocal = true
		} else if r.Host == "prod" {
			sawRemote = true
		}
	}
	if !sawLocal || !sawRemote {
		t.Errorf("rooms = %+v, want one local (empty host) and one remote (host=prod)", rooms.Rooms)
	}
}

// TestAPIBus_LoopbackGuard_StillRejectsLAN_WithRemoteConfigured proves
// success criterion 4 alongside a configured remote: the loopback gate does
// not loosen just because [bus.remotes] exists.
func TestAPIBus_LoopbackGuard_StillRejectsLAN_WithRemoteConfigured(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	home := busTestHome(t)
	writeRemotesConfig(t, home, "prod", baseURL, rec.Key)
	handler := newBusTestHandler(home, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/bus/rooms", nil)
	req.RemoteAddr = "192.168.1.50:4242"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("LAN peer with a remote configured = %d, want 403", rec2.Code)
	}
}
