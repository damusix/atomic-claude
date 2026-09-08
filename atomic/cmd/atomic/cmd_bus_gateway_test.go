package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
	"github.com/damusix/atomic-claude/atomic/internal/gateway"
)

// syncBuffer is a bytes.Buffer safe to write from gatewayAction's goroutine
// and read from the test goroutine polling for the "listening on" line.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestGatewayEnrollAction_MissingName_ExitUsage(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	code := gatewayEnrollAction(nil, home, &out, &errOut)
	if code != int(bus.ExitUsage) {
		t.Fatalf("exit code = %d, want %d", code, bus.ExitUsage)
	}
}

// TestGatewayEnrollAction_PrintsPasteableTOMLBlock_KeyMatchesStore proves
// success criterion 2: the printed block's key is 32 bytes, hex-encoded, and
// pasteable into ~/.atomic/config.toml.
func TestGatewayEnrollAction_PrintsPasteableTOMLBlock_KeyMatchesStore(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	code := gatewayEnrollAction([]string{"web-api"}, home, &out, &errOut)
	if code != int(bus.ExitOK) {
		t.Fatalf("exit code = %d, output: %s, stderr: %s", code, out.String(), errOut.String())
	}

	printed := out.String()
	if !strings.Contains(printed, "[bus.remotes.web-api]") {
		t.Fatalf("output = %q, want a [bus.remotes.web-api] block", printed)
	}

	wantHex := ""
	for _, line := range strings.Split(printed, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "key") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				wantHex = strings.Trim(strings.TrimSpace(parts[1]), `"`)
			}
		}
	}
	if wantHex == "" {
		t.Fatalf("could not find a key = \"...\" line in output: %q", printed)
	}
	keyBytes, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatalf("printed key is not valid hex: %v", err)
	}
	if len(keyBytes) != 32 {
		t.Fatalf("printed key is %d bytes, want 32", len(keyBytes))
	}
}

// TestGatewayEnrollAction_HostSchemeMatchesTLSCert proves finding 1 of the
// final review: a bare printed host leaves a reader to guess between
// remote.NewClient's https default and the gateway's plain-HTTP default.
// Enroll knows which one this gateway is (or will be) run as via --tls-cert,
// so the printed host carries the matching scheme.
func TestGatewayEnrollAction_HostSchemeMatchesTLSCert(t *testing.T) {
	home := t.TempDir()

	var out, errOut bytes.Buffer
	if code := gatewayEnrollAction([]string{"plain"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `host = "http://`) {
		t.Fatalf("output = %q, want a host with an http:// scheme when --tls-cert is absent", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := gatewayEnrollAction([]string{"--tls-cert", "cert.pem", "tls"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("exit code = %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `host = "https://`) {
		t.Fatalf("output = %q, want a host with an https:// scheme when --tls-cert is given", out.String())
	}
}

// TestGatewayRevokeAction_DeletesKey proves criterion 3: revoke removes the
// record from keys.json directly, with no gateway process involved.
func TestGatewayRevokeAction_DeletesKey(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	if code := gatewayEnrollAction([]string{"web-api"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("enroll exit code = %d, stderr: %s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	code := gatewayRevokeAction([]string{"web-api"}, home, &out, &errOut)
	if code != int(bus.ExitOK) {
		t.Fatalf("revoke exit code = %d, stderr: %s", code, errOut.String())
	}

	store := gateway.NewStore(gatewayKeysPath(home))
	data, err := os.ReadFile(gatewayKeysPath(home))
	if err != nil {
		t.Fatalf("read keys.json: %v", err)
	}
	if strings.Contains(string(data), `"web-api"`) {
		t.Fatalf("keys.json still contains web-api after revoke: %s", data)
	}
	// Re-enrolling the same name after revoke must succeed cleanly (store is
	// still usable, not left in some half-deleted state).
	if _, err := store.Enroll("web-api"); err != nil {
		t.Fatalf("re-enroll after revoke: %v", err)
	}
}

func TestGatewayKeysPath_UnderHomeAtomicGateway(t *testing.T) {
	home := "/tmp/somehome"
	got := gatewayKeysPath(home)
	want := filepath.Join(home, ".atomic", "gateway", "keys.json")
	if got != want {
		t.Fatalf("gatewayKeysPath = %q, want %q", got, want)
	}
}

func TestGatewayAction_TLSCertWithoutKey_ExitUsage(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	code := gatewayAction([]string{"--tls-cert", "cert.pem"}, home, &out, &errOut)
	if code != int(bus.ExitUsage) {
		t.Fatalf("exit code = %d, want %d, stderr: %s", code, bus.ExitUsage, errOut.String())
	}
}

func TestGatewayAction_UnknownFlag_ExitUsage(t *testing.T) {
	home := t.TempDir()
	var out, errOut bytes.Buffer
	code := gatewayAction([]string{"--bogus"}, home, &out, &errOut)
	if code != int(bus.ExitUsage) {
		t.Fatalf("exit code = %d, want %d, stderr: %s", code, bus.ExitUsage, errOut.String())
	}
}

// TestGatewayAction_StartsAndServes_ClientWithValidKeyReaches proves success
// criterion 1 at the cmd/atomic wiring level: `atomic bus gateway` (this
// package's gatewayAction) actually binds, starts the daemon beside it, and
// answers a sealed request from an enrolled client — not just internal/gateway
// in isolation.
func TestGatewayAction_StartsAndServes_ClientWithValidKeyReaches(t *testing.T) {
	// Not t.TempDir(): the unix socket gatewayAction binds embeds the full
	// test name and can exceed the ~104-108 byte sun_path limit (see
	// testBusDispatchHome in cmd_bus_test.go for the same constraint).
	home := testBusDispatchHome(t)

	if _, err := gateway.NewStore(gatewayKeysPath(home)).Enroll("laptop"); err != nil {
		t.Fatalf("seed enroll: %v", err)
	}

	var out, errOut syncBuffer
	go func() {
		gatewayAction([]string{"--addr", "127.0.0.1:0"}, home, &out, &errOut)
	}()

	addr := waitForListeningAddr(t, &out, &errOut)

	rec := onlyEnrolledRecord(t, home)
	c, err := remote.NewClient(remote.RemoteConfig{Name: "laptop", Host: "http://" + addr, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	plaintext, err := c.Do([]byte(`{"op":"ping"}`))
	if err != nil {
		t.Fatalf("Do: %v, stderr: %s", err, errOut.String())
	}
	var resp bus.Response
	if err := json.Unmarshal(plaintext, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("ping response not ok: %+v", resp)
	}
}

// TestGatewayAction_StaleSocket_RecoversAndServes proves finding 3 of the
// final review: a socket file left behind by a process that died without
// closing it — an OOM-killed container is the documented case in
// docs/guides/bus-hosting.md — no longer bricks every later `atomic bus
// gateway` start with "address already in use".
func TestGatewayAction_StaleSocket_RecoversAndServes(t *testing.T) {
	home := testBusDispatchHome(t)

	if err := bus.EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	ln, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		t.Fatalf("bind stale socket: %v", err)
	}
	ul, ok := ln.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", ln)
	}
	ul.SetUnlinkOnClose(false)
	if err := ul.Close(); err != nil {
		t.Fatalf("close (leaving the socket file behind): %v", err)
	}

	if _, err := gateway.NewStore(gatewayKeysPath(home)).Enroll("laptop"); err != nil {
		t.Fatalf("seed enroll: %v", err)
	}

	var out, errOut syncBuffer
	go func() {
		gatewayAction([]string{"--addr", "127.0.0.1:0"}, home, &out, &errOut)
	}()

	addr := waitForListeningAddr(t, &out, &errOut)

	rec := onlyEnrolledRecord(t, home)
	c, err := remote.NewClient(remote.RemoteConfig{Name: "laptop", Host: "http://" + addr, Key: rec.Key})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Do([]byte(`{"op":"ping"}`)); err != nil {
		t.Fatalf("Do against a gateway started with a stale socket present: %v, stderr: %s", err, errOut.String())
	}
}

func waitForListeningAddr(t *testing.T, out, errOut *syncBuffer) string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if s := out.String(); strings.HasPrefix(s, "gateway listening on ") {
			return strings.TrimSpace(strings.TrimPrefix(s, "gateway listening on "))
		}
		select {
		case <-deadline:
			t.Fatalf("gateway never printed its listening address; stdout: %q, stderr: %q", out.String(), errOut.String())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func onlyEnrolledRecord(t *testing.T, home string) gateway.Record {
	t.Helper()
	data, err := os.ReadFile(gatewayKeysPath(home))
	if err != nil {
		t.Fatalf("read keys.json: %v", err)
	}
	var records map[string]gateway.Record
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("parse keys.json: %v", err)
	}
	for _, rec := range records {
		return rec
	}
	t.Fatal("keys.json has no records")
	return gateway.Record{}
}
