// Package remote_test exercises Client against a real gateway.Server and bus
// daemon. It is a separate (black-box) package from remote because
// internal/gateway imports internal/bus/remote — an internal test file here
// pulling in internal/gateway would be an import cycle the Go toolchain
// refuses to build.
package remote_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
	"github.com/damusix/atomic-claude/atomic/internal/gateway"
)

// startTestGateway runs a real gateway.Server and bus daemon on live
// listeners and returns the gateway's base URL and key store. bus.Serve runs
// directly inside gateway.Run — never spawnServe, which under `go test` would
// re-run the whole suite.
func startTestGateway(t *testing.T) (baseURL string, store *gateway.Store) {
	t.Helper()

	unixDir, err := os.MkdirTemp("/tmp", "atomicremote")
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

// TestClient_Do_RoundTrip_ThroughRealGateway proves Do against the real
// admission ladder and daemon. Admission looks the frame's key_id up in the
// store exactly as gateway.Store.Enroll wrote it, so the round trip only
// succeeds if Client derives the identical id from the same key.
func TestClient_Do_RoundTrip_ThroughRealGateway(t *testing.T) {
	baseURL, store := startTestGateway(t)

	rec, err := store.Enroll("laptop")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	c, err := remote.NewClient(remote.RemoteConfig{Name: "laptop", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	plaintext, err := c.Do([]byte(`{"op":"ping"}`))
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	var resp bus.Response
	if err := json.Unmarshal(plaintext, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ping response not ok: %+v", resp)
	}
}

// writeRemotesConfig writes a [bus.remotes.<name>] table under home, matching
// the shape gateway.Store.Enroll prints for an operator to paste in.
func writeRemotesConfig(t *testing.T, home, name, host string, key []byte) {
	t.Helper()
	dir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	toml := fmt.Sprintf("[bus.remotes.%s]\nhost = %q\nkey  = %q\n", name, host, hex.EncodeToString(key))
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
}

// shortHome returns a temp dir short enough for <home>/.atomic/bus.sock to
// fit the ~104-byte unix socket path limit, which t.TempDir() can blow past
// on macOS.
func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atomicremote")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startLocalDaemon serves the local bus directly on home's own socket — no
// spawnServe, matching this package's ban on spawning a real daemon from a
// test. Once this is listening, EnsureDaemon's own connect-first probe finds
// it live and never reaches the spawn path either.
func startLocalDaemon(t *testing.T, home string) {
	t.Helper()
	if err := bus.EnsureDirs(home); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	ln, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Serve(ctx, ln, bus.NewHub(home), nil) }()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("local bus daemon did not exit after cancellation")
		}
	})
}

// TestReadAction_Host_ReturnsRemoteTranscript proves criterion 7: `read
// --host` recovers a remote room's transcript over OpRead, since a remote
// client reached through a gateway has no room log file to open directly.
func TestReadAction_Host_ReturnsRemoteTranscript(t *testing.T) {
	baseURL, store := startTestGateway(t)

	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	joinBody, err := json.Marshal(bus.Request{Op: bus.OpJoin, Room: "potato", Name: "seed", Kind: bus.KindAgent, Session: "sess-seed"})
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	if _, err := c.Do(joinBody); err != nil {
		t.Fatalf("Do(join): %v", err)
	}

	sayBody, err := json.Marshal(bus.Request{Op: bus.OpSay, Room: "potato", Text: "hello from prod"})
	if err != nil {
		t.Fatalf("marshal say: %v", err)
	}
	sayPlaintext, err := c.Do(sayBody)
	if err != nil {
		t.Fatalf("Do(say): %v", err)
	}
	var sayResp bus.Response
	if err := json.Unmarshal(sayPlaintext, &sayResp); err != nil {
		t.Fatalf("unmarshal say response: %v", err)
	}
	if !sayResp.OK {
		t.Fatalf("say response not ok: %+v", sayResp)
	}
	var sayPayload struct {
		Envelope bus.Envelope `json:"envelope"`
	}
	if err := json.Unmarshal(sayResp.Payload, &sayPayload); err != nil {
		t.Fatalf("unmarshal say payload: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	var out bytes.Buffer
	code := bus.BusAction([]string{"read", "potato", sayPayload.Envelope.ID, "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("read --host exit code = %d, output: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "hello from prod") {
		t.Fatalf("output = %q, want it to contain the remote transcript's text", out.String())
	}
}

// TestJoinAction_Host_RecordsHostAndLaterSendResolvesThere proves success
// criterion 4: `join --host prod` records the host in bus.json, and a later
// `send` with no flag resolves there with no daemon spawned locally.
func TestJoinAction_Host_RecordsHostAndLaterSendResolvesThere(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	var out bytes.Buffer
	code := bus.BusAction([]string{"join", "potato", "--as", "laptop", "--session", "sess-1", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("join --host exit code = %d, output: %s", code, out.String())
	}

	out.Reset()
	code = bus.BusAction([]string{"send", "potato", "hi from the client", "--session", "sess-1"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("send with no flag after a remote join exit code = %d, output: %s", code, out.String())
	}

	out.Reset()
	code = bus.BusAction([]string{"who", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("who --host exit code = %d, output: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "laptop") {
		t.Fatalf("who --host output = %q, want it to list the joined member", out.String())
	}
}

// TestLeaveAction_Host_RemovesRemoteMembership proves leave resolves the same
// membership host join recorded, with no --host repeated.
func TestLeaveAction_Host_RemovesRemoteMembership(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	// A second member keeps the room alive after "laptop" leaves — an empty
	// room is dropped by the daemon, which would make who --host fail on "no
	// such room" rather than proving the member is gone.
	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	seedBody, err := json.Marshal(bus.Request{Op: bus.OpJoin, Room: "potato", Name: "seed", Kind: bus.KindAgent, Session: "sess-seed"})
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	if _, err := c.Do(seedBody); err != nil {
		t.Fatalf("Do(join seed): %v", err)
	}

	var out bytes.Buffer
	if code := bus.BusAction([]string{"join", "potato", "--as", "laptop", "--session", "sess-1", "--host", "prod"}, cliHome, t.TempDir(), &out); code != int(bus.ExitOK) {
		t.Fatalf("join --host exit code = %d, output: %s", code, out.String())
	}

	out.Reset()
	code := bus.BusAction([]string{"leave", "potato", "--session", "sess-1"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("leave exit code = %d, output: %s", code, out.String())
	}

	out.Reset()
	code = bus.BusAction([]string{"who", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("who --host exit code = %d, output: %s", code, out.String())
	}
	if strings.Contains(out.String(), "laptop") {
		t.Fatalf("who --host output = %q, want the member gone after leave", out.String())
	}
}

// TestRoomsAction_Host_ListsRemoteRooms proves criterion 5's sibling: --host
// on a room-agnostic verb reaches that gateway directly.
func TestRoomsAction_Host_ListsRemoteRooms(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	joinBody, err := json.Marshal(bus.Request{Op: bus.OpJoin, Room: "potato", Name: "seed", Kind: bus.KindAgent, Session: "sess-seed"})
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	if _, err := c.Do(joinBody); err != nil {
		t.Fatalf("Do(join): %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	var out bytes.Buffer
	code := bus.BusAction([]string{"rooms", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("rooms --host exit code = %d, output: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "potato") {
		t.Fatalf("rooms --host output = %q, want it to list the remote room", out.String())
	}
}

// TestStatusAction_Host_ReportsRemoteDaemon proves --host on status pings
// that gateway's daemon instead of the local one.
func TestStatusAction_Host_ReportsRemoteDaemon(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	var out bytes.Buffer
	code := bus.BusAction([]string{"status", "--session", "sess-1", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("status --host exit code = %d, output: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "daemon: running") {
		t.Fatalf("status --host output = %q, want it to report the remote daemon running", out.String())
	}
}

// TestRecvAction_Host_DeliversPublishedMessage proves recv --host streams a
// remote room's traffic through the gateway rather than a local subscribe.
func TestRecvAction_Host_DeliversPublishedMessage(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)

	done := make(chan struct{})
	var out bytes.Buffer
	var code int
	go func() {
		code = bus.BusAction([]string{"recv", "potato", "--session", "sess-recv", "--host", "prod"}, cliHome, t.TempDir(), &out)
		close(done)
	}()

	// Give recv time to open its subscription before publishing.
	time.Sleep(200 * time.Millisecond)

	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sayBody, err := json.Marshal(bus.Request{Op: bus.OpSay, Room: "potato", Text: "hello over the wire"})
	if err != nil {
		t.Fatalf("marshal say: %v", err)
	}
	if _, err := c.Do(sayBody); err != nil {
		t.Fatalf("Do(say): %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("recv --host exited early (%d) before delivering: %s", code, out.String())
		case <-deadline:
			t.Fatalf("timed out waiting for recv --host to deliver, output so far: %s", out.String())
		default:
		}
		if strings.Contains(out.String(), "hello over the wire") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// remoteJoin seeds room on baseURL's gateway with a member, for tests that
// need a remote room to already exist before exercising a room-scoped verb.
func remoteJoin(t *testing.T, baseURL string, key []byte, room, name, session string) {
	t.Helper()
	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	body, err := json.Marshal(bus.Request{Op: bus.OpJoin, Room: room, Name: name, Kind: bus.KindAgent, Session: session})
	if err != nil {
		t.Fatalf("marshal join: %v", err)
	}
	if _, err := c.Do(body); err != nil {
		t.Fatalf("Do(join %s): %v", room, err)
	}
}

// remoteWhoOutput runs `bus who <room> --host prod` and returns its text
// output, for tests that only need to observe remote room state.
func remoteWhoOutput(t *testing.T, cliHome, room string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	code := bus.BusAction([]string{"who", room, "--host", "prod"}, cliHome, t.TempDir(), &out)
	return out.String(), code
}

// TestSayAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched proves
// criterion 9's fix: before --host routed say, Hub.PublishAsOperator resolved
// by bare room name, so an operator's `say` against a room name that also
// exists locally would have published into the local room instead of the
// intended remote one, with no error.
func TestSayAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := shortHome(t)
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	startLocalDaemon(t, cliHome)

	var out bytes.Buffer
	if code := bus.BusAction([]string{"join", "potato", "--as", "local-member", "--session", "sess-local"}, cliHome, t.TempDir(), &out); code != int(bus.ExitOK) {
		t.Fatalf("local join exit code = %d, output: %s", code, out.String())
	}
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	out.Reset()
	code := bus.BusAction([]string{"say", "potato", "hello remote", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("say --host exit code = %d, output: %s", code, out.String())
	}
	id := strings.TrimSuffix(strings.SplitN(out.String(), "(id ", 2)[1], ")\n")

	out.Reset()
	code = bus.BusAction([]string{"read", "potato", id, "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) || !strings.Contains(out.String(), "hello remote") {
		t.Fatalf("remote room did not receive the say: exit=%d output=%q", code, out.String())
	}

	// No local send has ever happened in room "potato", so a log existing at
	// all — let alone containing the remote text — would itself prove the
	// wrong-target bug.
	localLog, err := os.ReadFile(bus.RoomLogPath(cliHome, "potato"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read local room log: %v", err)
	}
	if strings.Contains(string(localLog), "hello remote") {
		t.Fatalf("local room %q log contains the remote say — wrong-target bug: %s", "potato", localLog)
	}
}

// TestHaltAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched proves
// the same fix for halt: Hub.setHalted resolves by bare room name, so this is
// the same wrong-target hazard as say's, for the operator control that stops
// a room's traffic.
func TestHaltAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := shortHome(t)
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	startLocalDaemon(t, cliHome)

	var out bytes.Buffer
	if code := bus.BusAction([]string{"join", "potato", "--as", "local-member", "--session", "sess-local"}, cliHome, t.TempDir(), &out); code != int(bus.ExitOK) {
		t.Fatalf("local join exit code = %d, output: %s", code, out.String())
	}
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	out.Reset()
	code := bus.BusAction([]string{"halt", "potato", "--text", "maintenance", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("halt --host exit code = %d, output: %s", code, out.String())
	}

	remoteOut, remoteCode := remoteWhoOutput(t, cliHome, "potato")
	if remoteCode != int(bus.ExitOK) || !strings.Contains(remoteOut, "HALTED") {
		t.Fatalf("remote room was not halted: exit=%d output=%q", remoteCode, remoteOut)
	}

	out.Reset()
	code = bus.BusAction([]string{"who", "potato"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("local who exit code = %d, output: %s", code, out.String())
	}
	if strings.Contains(out.String(), "HALTED") {
		t.Fatalf("local room %q was halted — wrong-target bug: %s", "potato", out.String())
	}
}

// TestResumeAction_Host_ReachesRemote proves resume routes through --host the
// same way halt does.
func TestResumeAction_Host_ReachesRemote(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	var out bytes.Buffer
	if code := bus.BusAction([]string{"halt", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out); code != int(bus.ExitOK) {
		t.Fatalf("halt --host exit code = %d, output: %s", code, out.String())
	}

	out.Reset()
	code := bus.BusAction([]string{"resume", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("resume --host exit code = %d, output: %s", code, out.String())
	}

	remoteOut, remoteCode := remoteWhoOutput(t, cliHome, "potato")
	if remoteCode != int(bus.ExitOK) || strings.Contains(remoteOut, "HALTED") {
		t.Fatalf("remote room still halted after resume --host: exit=%d output=%q", remoteCode, remoteOut)
	}
}

// TestPruneAction_Host_ReachesRemote proves prune reaches the remote gateway
// rather than erroring on a missing local daemon or --host usage.
func TestPruneAction_Host_ReachesRemote(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	var out bytes.Buffer
	code := bus.BusAction([]string{"prune", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("prune --host exit code = %d, output: %s", code, out.String())
	}
}

// TestCloseAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched
// proves close routes through --host, and that dropping the remote room
// leaves a same-named local room (and its own bus.json membership) alone.
func TestCloseAction_Host_ReachesRemoteAndLeavesSameNameLocalRoomUntouched(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := shortHome(t)
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	startLocalDaemon(t, cliHome)

	var out bytes.Buffer
	if code := bus.BusAction([]string{"join", "potato", "--as", "local-member", "--session", "sess-local"}, cliHome, t.TempDir(), &out); code != int(bus.ExitOK) {
		t.Fatalf("local join exit code = %d, output: %s", code, out.String())
	}
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	out.Reset()
	code := bus.BusAction([]string{"close", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("close --host exit code = %d, output: %s", code, out.String())
	}

	_, remoteCode := remoteWhoOutput(t, cliHome, "potato")
	if remoteCode != int(bus.ExitNoRoom) {
		t.Fatalf("remote room %q still exists after close --host, exit=%d", "potato", remoteCode)
	}

	out.Reset()
	code = bus.BusAction([]string{"who", "potato"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) || !strings.Contains(out.String(), "local-member") {
		t.Fatalf("local room %q was affected by close --host — wrong-target bug: exit=%d output=%q", "potato", code, out.String())
	}
}

// TestEndAction_Host_ReachesRemote proves end routes through --host and evicts
// the named remote member.
func TestEndAction_Host_ReachesRemote(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")
	remoteJoin(t, baseURL, rec.Key, "potato", "evictee", "sess-evict")

	var out bytes.Buffer
	code := bus.BusAction([]string{"end", "potato", "evictee", "--host", "prod"}, cliHome, t.TempDir(), &out)
	if code != int(bus.ExitOK) {
		t.Fatalf("end --host exit code = %d, output: %s", code, out.String())
	}

	remoteOut, remoteCode := remoteWhoOutput(t, cliHome, "potato")
	if remoteCode != int(bus.ExitOK) || strings.Contains(remoteOut, "evictee") {
		t.Fatalf("evictee still a remote member after end --host: exit=%d output=%q", remoteCode, remoteOut)
	}
}

// TestTailAction_Host_StreamsRemoteRoom proves tail --host subscribes through
// the gateway rather than the local daemon.
func TestTailAction_Host_StreamsRemoteRoom(t *testing.T) {
	baseURL, store := startTestGateway(t)
	rec, err := store.Enroll("prod")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	cliHome := t.TempDir()
	writeRemotesConfig(t, cliHome, "prod", baseURL, rec.Key)
	remoteJoin(t, baseURL, rec.Key, "potato", "seed", "sess-seed")

	done := make(chan struct{})
	var out bytes.Buffer
	var code int
	go func() {
		code = bus.BusAction([]string{"tail", "potato", "--host", "prod"}, cliHome, t.TempDir(), &out)
		close(done)
	}()

	// Give tail time to open its subscription before publishing.
	time.Sleep(200 * time.Millisecond)

	c, err := remote.NewClient(remote.RemoteConfig{Name: "prod", Host: baseURL, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	sayBody, err := json.Marshal(bus.Request{Op: bus.OpSay, Room: "potato", Text: "tail sees this"})
	if err != nil {
		t.Fatalf("marshal say: %v", err)
	}
	if _, err := c.Do(sayBody); err != nil {
		t.Fatalf("Do(say): %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-done:
			t.Fatalf("tail --host exited early (%d) before delivering: %s", code, out.String())
		case <-deadline:
			t.Fatalf("timed out waiting for tail --host to deliver, output so far: %s", out.String())
		default:
		}
		if strings.Contains(out.String(), "tail sees this") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
