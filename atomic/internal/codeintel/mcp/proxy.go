// Proxy implements the client-side `atomic code mcp` path (master CP23).
//
// RunProxy is the new implementation of `atomic code mcp`: it connect-or-starts
// the singleton daemon (flock-guarded) and then bidirectionally pipes
// stdin↔socket / stdout↔socket.
//
// The daemon entry point is RunDaemon, invoked by the internal `atomic code __serve`
// verb (see code.go). That verb is NOT advertised in /atomic-help; it is called
// only by the auto-start path.
package mcp

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// Spawn seam (injectable for tests)
// ---------------------------------------------------------------------------

// SpawnFunc is the function called by the proxy to start the daemon when the
// socket is absent or dead. The real implementation forks a detached
// `atomic code __serve <projectRoot>` subprocess. Tests inject an in-process
// stub that starts a goroutine daemon instead.
type SpawnFunc func(projectRoot string) error

// DefaultSpawn starts `atomic code __serve <projectRoot>` detached (no TTY,
// stdout/stderr to devnull so the parent can exit immediately).
func DefaultSpawn(projectRoot string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("spawn daemon: locate executable: %w", err)
	}
	cmd := exec.Command(self, "code", "__serve", projectRoot)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Detach from the parent's process group so the daemon survives the proxy exit.
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// ---------------------------------------------------------------------------
// Auto-start (flock-guarded)
// ---------------------------------------------------------------------------

// EnsureRunning ensures the daemon is running, starting it if necessary.
// It uses an flock on LockPath to prevent a thundering herd:
//
//  1. Try connecting — if it works, we are done.
//  2. Acquire the flock.
//  3. Re-check the socket (another goroutine/process may have won).
//  4. Remove stale socket file if present.
//  5. Invoke spawn(projectRoot).
//  6. Retry connect with bounded backoff.
//
// Exported so tests can exercise the auto-start logic directly.
func EnsureRunning(ctx context.Context, projectRoot string, spawn SpawnFunc) error {
	return ensureRunning(ctx, projectRoot, spawn)
}

func ensureRunning(ctx context.Context, projectRoot string, spawn SpawnFunc) error {
	sockPath := SocketPath(projectRoot)

	// Fast path: daemon already running.
	if IsLive(sockPath) {
		return nil
	}

	// Acquire the flock to serialise concurrent starters.
	lockPath := LockPath(projectRoot)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return fmt.Errorf("ensure daemon: mkdir lock dir: %w", err)
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("ensure daemon: open lock file: %w", err)
	}
	defer lockFile.Close()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("ensure daemon: flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	// Re-check: another starter may have won the race.
	if IsLive(sockPath) {
		return nil
	}

	// Remove stale socket file (server died, file left behind).
	_ = os.Remove(sockPath)

	// Spawn the daemon detached.
	if err := spawn(projectRoot); err != nil {
		return fmt.Errorf("ensure daemon: spawn: %w", err)
	}

	// Retry connect with bounded backoff (max ~5 s).
	return waitLive(ctx, sockPath)
}

// waitLive polls the socket until it becomes connectable or ctx is done.
func waitLive(ctx context.Context, sockPath string) error {
	backoff := 50 * time.Millisecond
	const maxBackoff = 500 * time.Millisecond
	const maxTotal = 10 * time.Second

	deadline := time.Now().Add(maxTotal)
	for {
		if IsLive(sockPath) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("daemon did not start within %v", maxTotal)
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// ---------------------------------------------------------------------------
// Proxy (bidirectional byte pipe)
// ---------------------------------------------------------------------------

// RunProxy implements `atomic code mcp`:
//
//  1. Ensure the daemon is running (flock-guarded auto-start via spawn).
//  2. Connect to the unix socket.
//  3. Bidirectionally pipe stdin→socket and socket→stdout.
//
// When the MCP client disconnects (stdin closes), the proxy exits. The daemon
// stays alive so a second `atomic code mcp` invocation reuses the warm engine.
func RunProxy(ctx context.Context, projectRoot string, spawn SpawnFunc, stdin io.Reader, stdout io.Writer) error {
	if spawn == nil {
		spawn = DefaultSpawn
	}

	if err := ensureRunning(ctx, projectRoot, spawn); err != nil {
		return fmt.Errorf("atomic code mcp: %w", err)
	}

	sockPath := SocketPath(projectRoot)
	conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("atomic code mcp: connect daemon: %w", err)
	}
	defer conn.Close()

	// Bidirectional pipe: stdin→socket and socket→stdout, simultaneously.
	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(conn, stdin)
		// Signal the other direction to stop by half-closing the write side.
		// conn is always a *net.UnixConn here (we just dialed a unix socket),
		// but assert explicitly so a wrapped conn that supports CloseWrite via
		// a different interface is also handled; log the miss rather than
		// silently skipping half-close.
		type halfCloser interface {
			CloseWrite() error
		}
		if hc, ok := conn.(halfCloser); ok {
			_ = hc.CloseWrite()
		} else {
			// Fallback: close the whole connection so the read goroutine unblocks.
			_ = conn.Close()
		}
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(stdout, conn)
		errCh <- err
	}()

	// Wait for either direction to finish (client disconnect or daemon exit).
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-errCh:
		return nil
	}
}
