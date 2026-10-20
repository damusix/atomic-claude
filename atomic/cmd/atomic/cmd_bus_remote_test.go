package main

import (
	"bytes"
	"encoding/hex"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/gateway"
	"github.com/pelletier/go-toml/v2"
)

var testRemoteKey = strings.Repeat("ab", 32)

func stubBusRemoteTTY(t *testing.T, tty bool, form func(*busRemoteFields, string, func(string) bool) error) {
	t.Helper()
	origTTY, origForm := busRemoteIsTTY, busRemoteForm
	busRemoteIsTTY = func() bool { return tty }
	if form != nil {
		busRemoteForm = form
	}
	t.Cleanup(func() { busRemoteIsTTY, busRemoteForm = origTTY, origForm })
}

// The point of `remote add` is that --host <name> works right after it, so the
// check reads back through the bus's own loader, not the config struct.
func TestBusRemoteAdd_FlagsWriteWhatTheBusReads(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	var out, errOut bytes.Buffer
	code := busRemoteAddAction([]string{"web-api", "--host", "http://10.0.0.5:7777", "--key", testRemoteKey}, home, &out, &errOut)
	if code != int(bus.ExitOK) {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	remotes, err := remote.Remotes(home)
	if err != nil {
		t.Fatalf("remote.Remotes: %v", err)
	}
	r, ok := remotes["web-api"]
	if !ok || r.Host != "http://10.0.0.5:7777" || len(r.Key) != 32 {
		t.Fatalf("remote.Remotes()[web-api] = %+v (ok=%v)", r, ok)
	}
	// config.toml now holds a credential.
	info, err := os.Stat(config.TOMLPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config.toml mode = %o, want 600", info.Mode().Perm())
	}
}

// The bus dials from whatever directory a session runs in.
func TestBusRemoteAdd_RelativeCAIsStoredAbsolute(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ca.pem"), selfSignedPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var out, errOut bytes.Buffer
	if code := busRemoteAddAction([]string{"web", "--host", "a.example.com", "--key", testRemoteKey, "--ca", "ca.pem"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	cfg, _, _ := config.Load(config.TOMLPath(home))
	if ca := cfg.Bus.Remotes["web"].CA; !filepath.IsAbs(ca) {
		t.Errorf("stored ca = %q, want absolute", ca)
	}
}

func selfSignedPEM(t *testing.T) []byte {
	t.Helper()
	srv := httptest.NewTLSServer(nil)
	t.Cleanup(srv.Close)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

// Scripts and CI have no terminal; a missing field must fail fast rather than
// hang on a form nobody can answer.
func TestBusRemoteAdd_MissingFieldWithoutTTYIsUsage(t *testing.T) {
	stubBusRemoteTTY(t, false, func(*busRemoteFields, string, func(string) bool) error {
		t.Fatal("form must not run without a terminal")
		return nil
	})
	var out, errOut bytes.Buffer
	if code := busRemoteAddAction([]string{"web-api", "--host", "bus.example.com"}, t.TempDir(), &out, &errOut); code != int(bus.ExitUsage) {
		t.Fatalf("exit %d, want usage", code)
	}
}

func TestBusRemoteAdd_FormFillsMissingFields(t *testing.T) {
	var seen busRemoteFields
	stubBusRemoteTTY(t, true, func(f *busRemoteFields, _ string, _ func(string) bool) error {
		seen = *f
		f.Name = "lan"
		f.Key = testRemoteKey
		return nil
	})
	home := t.TempDir()
	var out, errOut bytes.Buffer
	if code := busRemoteAddAction([]string{"--host", "http://10.0.0.5:7777"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if seen.Host != "http://10.0.0.5:7777" {
		t.Errorf("form was not prefilled from --host: %+v", seen)
	}
	cfg, _, _ := config.Load(config.TOMLPath(home))
	if cfg.Bus.Remotes["lan"].Key != testRemoteKey {
		t.Errorf("form result not saved: %+v", cfg.Bus.Remotes)
	}
}

func TestBusRemoteAdd_TakenNameNeedsForce(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	args := []string{"web", "--host", "a.example.com", "--key", testRemoteKey}
	var out, errOut bytes.Buffer
	if code := busRemoteAddAction(args, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("first add exit %d: %s", code, errOut.String())
	}
	if code := busRemoteAddAction(args, home, &out, &errOut); code != int(bus.ExitNameTaken) {
		t.Fatalf("second add exit %d, want name taken", code)
	}
	if code := busRemoteAddAction(append(args, "--force"), home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("forced add exit %d: %s", code, errOut.String())
	}
}

func TestBusRemoteAdd_BadKeyIsRejected(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	var out, errOut bytes.Buffer
	if code := busRemoteAddAction([]string{"web", "--host", "a.example.com", "--key", "abcd"}, t.TempDir(), &out, &errOut); code != int(bus.ExitUsage) {
		t.Fatalf("exit %d, want usage", code)
	}
}

// The key is a credential; list output lands in terminals and logs.
func TestBusRemoteList_NeverPrintsKey(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	var out, errOut bytes.Buffer
	busRemoteAddAction([]string{"web", "--host", "a.example.com", "--key", testRemoteKey}, home, &out, &errOut)
	for _, args := range [][]string{nil, {"--json"}} {
		out.Reset()
		if code := busRemoteListAction(args, home, &out, &errOut); code != int(bus.ExitOK) {
			t.Fatalf("list %v exit %d: %s", args, code, errOut.String())
		}
		if !strings.Contains(out.String(), "a.example.com") {
			t.Errorf("list %v missing host: %q", args, out.String())
		}
		if strings.Contains(out.String(), testRemoteKey) {
			t.Errorf("list %v printed the key: %q", args, out.String())
		}
	}
}

func TestBusRemoteRemove(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	var out, errOut bytes.Buffer
	busRemoteAddAction([]string{"web", "--host", "a.example.com", "--key", testRemoteKey}, home, &out, &errOut)
	if code := busRemoteRemoveAction([]string{"web"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("remove exit %d: %s", code, errOut.String())
	}
	if code := busRemoteRemoveAction([]string{"web"}, home, &out, &errOut); code != int(bus.ExitUsage) {
		t.Fatalf("second remove exit %d, want usage (unknown name, same as remote test)", code)
	}
	remotes, _ := remote.Remotes(home)
	if len(remotes) != 0 {
		t.Fatalf("remote still configured: %v", remotes)
	}
}

// `remote test` exists so a key or address mistake shows up before a session
// relies on the remote, so it has to tell a working entry from a wrong key.
func TestBusRemoteTest_LiveGateway(t *testing.T) {
	gwHome := testBusDispatchHome(t)
	if _, err := gateway.NewStore(gatewayKeysPath(gwHome)).Enroll("laptop"); err != nil {
		t.Fatal(err)
	}
	var gwOut, gwErr syncBuffer
	go gatewayAction([]string{"--addr", "127.0.0.1:0"}, gwHome, &gwOut, &gwErr)
	addr := waitForListeningAddr(t, &gwOut, &gwErr)
	good := hex.EncodeToString(onlyEnrolledRecord(t, gwHome).Key)

	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	var out, errOut bytes.Buffer
	busRemoteAddAction([]string{"good", "--host", "http://" + addr, "--key", good}, home, &out, &errOut)
	busRemoteAddAction([]string{"wrongkey", "--host", "http://" + addr, "--key", testRemoteKey}, home, &out, &errOut)

	out.Reset()
	if code := busRemoteTestAction([]string{"good"}, home, &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("test good exit %d: %s%s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("test good output = %q, want ok", out.String())
	}

	out.Reset()
	if code := busRemoteTestAction(nil, home, &out, &errOut); code != int(bus.ExitUnreachable) {
		t.Fatalf("test all exit %d, want unreachable because wrongkey fails: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "good") || !strings.Contains(out.String(), "wrongkey") {
		t.Errorf("test all must report every remote, got %q", out.String())
	}
}

func TestBusRemoteTest_UnreachableUnknownAndEmpty(t *testing.T) {
	stubBusRemoteTTY(t, false, nil)
	home := t.TempDir()
	var out, errOut bytes.Buffer
	if code := busRemoteTestAction(nil, home, &out, &errOut); code != int(bus.ExitUsage) {
		t.Errorf("test with nothing configured exit %d, want usage (a vacuous pass would hide a missing setup)", code)
	}
	busRemoteAddAction([]string{"down", "--host", "http://127.0.0.1:1", "--key", testRemoteKey}, home, &out, &errOut)
	if code := busRemoteTestAction([]string{"down"}, home, &out, &errOut); code != int(bus.ExitUnreachable) {
		t.Errorf("test down exit %d, want unreachable", code)
	}
	if code := busRemoteTestAction([]string{"nosuch"}, home, &out, &errOut); code != int(bus.ExitUsage) {
		t.Errorf("test unknown exit %d, want usage", code)
	}
}

// enroll's one-liner must be accepted by `remote add` once the placeholder host
// is filled in, or the copy-paste path is broken.
func TestGatewayEnrollPrintsRemoteAddLine(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := gatewayEnrollAction([]string{"laptop"}, t.TempDir(), &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("enroll exit %d: %s", code, errOut.String())
	}
	if code := gatewayEnrollAction([]string{"my.box"}, t.TempDir(), &bytes.Buffer{}, &bytes.Buffer{}); code != int(bus.ExitUsage) {
		t.Errorf("enroll of a name remote add would refuse: exit %d, want usage", code)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("enroll output must stay pasteable TOML: %v\n%s", err, out.String())
	}
	var line string
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Contains(l, "atomic bus remote add") {
			line = strings.TrimSpace(strings.TrimPrefix(l, "#"))
		}
	}
	if line == "" {
		t.Fatalf("enroll output has no remote add line: %q", out.String())
	}
	args := strings.Fields(strings.Replace(line, "<host:port>", "10.0.0.5:7777", 1))[4:]
	stubBusRemoteTTY(t, false, nil)
	if code := busRemoteAddAction(args, t.TempDir(), &out, &errOut); code != int(bus.ExitOK) {
		t.Fatalf("remote add %v exit %d: %s", args, code, errOut.String())
	}
}
