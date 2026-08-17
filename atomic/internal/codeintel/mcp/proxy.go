// Client side of `atomic code mcp`: connect-or-start the singleton daemon under
// an flock, then pipe stdio to its socket. Daemon mode is the same `mcp` verb
// with --daemon, an unadvertised flag existing only for the auto-start path.
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

// WatchOptions carries the sync-poller flags forwarded to the daemon on spawn.
type WatchOptions struct {
	// Disable turns the poller off entirely (--no-watch).
	Disable bool
	// Interval overrides SyncInterval when non-zero (--watch-interval).
	Interval time.Duration
}

// SpawnFunc starts the daemon when the socket is absent or dead. Production
// forks a detached subprocess; tests inject an in-process goroutine instead.
type SpawnFunc func(sourceRoot, dbPath string, opts WatchOptions) error

// DaemonArgv spawns through the registered `code mcp` verb rather than a
// separate internal one, so the spawned command can never drift out of the
// Cobra tree. Exported so tests can assert the exact argv.
func DaemonArgv(sourceRoot, dbPath string, opts WatchOptions) []string {
	argv := []string{"code", "mcp", "--daemon", "--source", sourceRoot, "--db", dbPath}
	if opts.Disable {
		argv = append(argv, "--no-watch")
	} else if opts.Interval > 0 {
		argv = append(argv, "--watch-interval", opts.Interval.String())
	}
	return argv
}

// DefaultSpawn starts the daemon detached with no stdio, so the parent can exit
// immediately. Explicit paths keep the daemon from re-resolving scope from cwd.
func DefaultSpawn(sourceRoot, dbPath string, opts WatchOptions) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("spawn daemon: locate executable: %w", err)
	}
	cmd := exec.Command(self, DaemonArgv(sourceRoot, dbPath, opts)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// New session, so the daemon outlives the proxy that spawned it.
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// EnsureRunning starts the daemon if it is not already up, serialising
// concurrent starters behind an flock so a burst of clients spawns one daemon
// rather than a herd. Exported so tests can drive the auto-start path.
func EnsureRunning(ctx context.Context, sourceRoot, dbPath string, spawn SpawnFunc) error {
	return ensureRunning(ctx, sourceRoot, dbPath, WatchOptions{}, spawn)
}

func ensureRunning(ctx context.Context, sourceRoot, dbPath string, opts WatchOptions, spawn SpawnFunc) error {
	sockPath := SocketPathFromDB(dbPath)

	if IsLive(sockPath) {
		return nil
	}

	lockPath := LockPathFromDB(dbPath)
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

	// Whoever held the lock before us may have started it already.
	if IsLive(sockPath) {
		return nil
	}

	// A socket file left behind by a dead server would block Listen.
	_ = os.Remove(sockPath)

	if err := spawn(sourceRoot, dbPath, opts); err != nil {
		return fmt.Errorf("ensure daemon: spawn: %w", err)
	}

	return waitLive(ctx, sockPath)
}

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

// RunProxy pipes stdio to the daemon's socket, which is derived from dbPath so
// proxy and daemon always agree on the path. On client disconnect the proxy
// exits but the daemon stays up, so the next invocation reuses a warm engine.
func RunProxy(ctx context.Context, sourceRoot, dbPath string, opts WatchOptions, spawn SpawnFunc, stdin io.Reader, stdout io.Writer) error {
	if spawn == nil {
		spawn = DefaultSpawn
	}

	if err := ensureRunning(ctx, sourceRoot, dbPath, opts, spawn); err != nil {
		return fmt.Errorf("atomic code mcp: %w", err)
	}

	sockPath := SocketPathFromDB(dbPath)
	conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("atomic code mcp: connect daemon: %w", err)
	}
	defer conn.Close()

	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(conn, stdin)
		// Half-close signals EOF to the daemon without killing the read side.
		type halfCloser interface {
			CloseWrite() error
		}
		if hc, ok := conn.(halfCloser); ok {
			_ = hc.CloseWrite()
		} else {
			// No half-close available: a full close at least unblocks the reader.
			_ = conn.Close()
		}
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(stdout, conn)
		errCh <- err
	}()

	// Either direction finishing means the session is over.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-errCh:
		return nil
	}
}
