// The only test here that execs the shipped binary. In-process tests cannot see
// argv assembly through the real Cobra tree or whether a spawned subprocess
// binds its socket — the seam a green suite once hid a broken auto-start behind.
package mcp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	codemcp "github.com/damusix/atomic-claude/atomic/internal/codeintel/mcp"
)

const (
	// Generous: a cold module cache links tree-sitter and wazero too.
	e2eBuildTimeout = 5 * time.Minute
	e2eRunTimeout   = 30 * time.Second
	e2eSocketWait   = 10 * time.Second
)

// Runs exactly the argv DefaultSpawn produces. When that argv named an
// unregistered verb, Cobra rejected it and no socket ever appeared.
func TestMCPDaemonBinary_StartsAndBindsSocket(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not on PATH: %v", err)
	}

	bin := buildAtomicBinaryForMCPE2E(t)

	// /tmp-rooted because the socket path derives from this one, and t.TempDir()
	// embeds the full test name — long enough to blow the sun_path limit.
	srcDir := shortE2ETempDir(t)
	dbPath := filepath.Join(shortE2ETempDir(t), "atomic.db")

	argv := codemcp.DaemonArgv(srcDir, dbPath, codemcp.WatchOptions{Disable: true})

	ctx, cancel := context.WithTimeout(context.Background(), e2eRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Dir = srcDir
	var out []byte
	outFile, err := os.CreateTemp(t.TempDir(), "daemon-out")
	if err != nil {
		t.Fatalf("create daemon output capture file: %v", err)
	}
	defer outFile.Close()
	cmd.Stdout = outFile
	cmd.Stderr = outFile

	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon subprocess (%v %v): %v", bin, argv, err)
	}
	// Fires the moment the subprocess exits, so a rejected argv fails fast
	// instead of burning the socket-wait budget.
	exited := make(chan *os.ProcessState, 1)
	go func() {
		_ = cmd.Wait()
		exited <- cmd.ProcessState
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-exited
	})

	sockPath := codemcp.SocketPathFromDB(dbPath)
	deadline := time.Now().Add(e2eSocketWait)
	for {
		if codemcp.IsLive(sockPath) {
			return
		}
		select {
		case state := <-exited:
			exited <- state // let the cleanup goroutine's <-exited also observe it
			out, _ = os.ReadFile(outFile.Name())
			t.Fatalf("daemon subprocess exited (code %d) before binding its socket at %s; output:\n%s",
				state.ExitCode(), sockPath, out)
		default:
		}
		if time.Now().After(deadline) {
			out, _ = os.ReadFile(outFile.Name())
			t.Fatalf("daemon did not bind its socket at %s within %s; output so far:\n%s", sockPath, e2eSocketWait, out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// shortE2ETempDir avoids t.TempDir(), whose nested test name can push a derived
// socket path past the unix sun_path limit.
func shortE2ETempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atmc-mcp-e2e")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func buildAtomicBinaryForMCPE2E(t *testing.T) string {
	t.Helper()

	moduleRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	out := filepath.Join(t.TempDir(), "atomic")

	ctx, cancel := context.WithTimeout(context.Background(), e2eBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", out, "./cmd/atomic")
	cmd.Dir = moduleRoot
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/atomic: %v\n%s", err, combined)
	}
	return out
}
