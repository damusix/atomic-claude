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

// Deliberately not t.TempDir(): that path embeds the full test name and can
// exceed the ~104-108 byte sun_path limit for a Unix domain socket.
func testBusDispatchHome(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "atomicbus-dispatch")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// internal/bus tests inject home straight into State.Load/Save, so they never
// exercise the os.UserHomeDir()-to-home hand-off inside runBus. Here a real
// daemon binds at bus.SocketPath(home) and a member is seeded via bus.Dial;
// a subprocess then runs with only HOME redirected. Resolving the wrong path
// (home+"/.claude", say) fails to dial and exits 6 rather than quietly
// returning an empty roster. runBus calls os.Exit, hence the subprocess.
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
