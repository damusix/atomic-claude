package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
)

// testBusDispatchHome creates a short, /tmp-rooted (not t.TempDir()) home
// directory for this test's real Unix domain socket, mirroring
// internal/bus/client_test.go's testBusHome — t.TempDir() embeds the full
// test name and can exceed the ~104-108 byte sun_path limit on macOS/Linux.
func testBusDispatchHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "atomicbus-dispatch")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestRunBus_DispatchUsesRealHomeFromEnv is the mandatory dispatch-layer
// real-filesystem test the checkpoint 4 brief requires: checkpoint 1's own
// disk test (internal/bus/identity_test.go) injects home directly into
// State.Load/Save, so it can never observe the os.UserHomeDir()-to-home
// hand-off inside runBus — that hand-off is only reachable, and only
// breakable, here.
//
// A real daemon is bound at bus.SocketPath(home) in this process, and a
// member is seeded on it directly (bypassing the CLI entirely, via
// bus.Dial). The subprocess then runs `atomic bus who dispatch-room --json`
// with HOME redirected to that same home and nothing else — if runBus
// resolved the wrong path (e.g. home+"/.claude", the scope-root class of
// bug .claude/skills/atomic-cli-contrib/SKILL.md §3-4 warns about), the
// subprocess would fail to dial the real socket and exit 6, not merely
// return an empty roster. runBus calls os.Exit, so it is exercised in a
// subprocess (the standard Go idiom for os.Exit-calling code), matching
// TestRunProfile_UsesHomeNotClaudeHome's established pattern.
func TestRunBus_DispatchUsesRealHomeFromEnv(t *testing.T) {
	if os.Getenv("ATOMIC_TEST_RUN_BUS_HELPER") == "1" {
		runBus([]string{"who", "dispatch-room", "--json"})
		return
	}

	home := testBusDispatchHome(t)
	if err := bus.EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	ln, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hub := bus.NewHub(home)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bus.Serve(ctx, ln, hub, nil) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("daemon did not shut down within the bounded wait")
		}
	})

	client, err := bus.Dial(home, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := client.Do(bus.Request{
		Op: bus.OpJoin, Room: "dispatch-room", Name: "probe", Kind: bus.KindAgent, Session: "sess-probe",
	}); err != nil {
		t.Fatalf("seed join: %v", err)
	}
	client.Close()

	cmd := exec.Command(os.Args[0], "-test.run=TestRunBus_DispatchUsesRealHomeFromEnv")
	cmd.Env = append(os.Environ(), "ATOMIC_TEST_RUN_BUS_HELPER=1", "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess runBus failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), `"probe"`) {
		t.Errorf("subprocess output does not contain the seeded member; got:\n%s", out)
	}
}
