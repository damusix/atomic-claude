package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Client is one connection to the daemon's Unix socket. Do performs a single
// round trip; Subscribe performs the same round trip then streams Envelope
// frames until the connection closes. Dial connects without any health check;
// EnsureDaemon is the guarded path that guarantees a live, version-matched
// daemon first.
//
// A Client is one-shot — good for exactly one Do or one Subscribe — because
// the daemon closes the connection after every non-subscription round trip.
type Client struct {
	conn net.Conn
	dec  *json.Decoder

	// timeout bounds Do's round trip and Subscribe's opening handshake, never
	// the live stream that follows, which legitimately waits indefinitely for
	// the next message. <= 0 disables the deadline.
	timeout time.Duration

	closeOnce sync.Once
	closed    chan struct{}
}

func newClient(conn net.Conn, timeout time.Duration) *Client {
	return &Client{
		conn:    conn,
		dec:     json.NewDecoder(conn),
		timeout: timeout,
		closed:  make(chan struct{}),
	}
}

// Dial opens a connection to SocketPath(home) with no probe-and-spawn behavior
// of its own — callers needing the daemon guaranteed running want EnsureDaemon.
func Dial(home string, timeout time.Duration) (*Client, error) {
	addr := SocketPath(home)
	var conn net.Conn
	var err error
	if timeout > 0 {
		conn, err = net.DialTimeout("unix", addr, timeout)
	} else {
		conn, err = net.Dial("unix", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("bus: dial %s: %w", addr, err)
	}
	return newClient(conn, timeout), nil
}

// Do sends req and returns the daemon's Response. A {"ok":false} reply also
// returns an *Error carrying the Code the daemon assigned — the client never
// re-derives an exit code from message text. Do consumes c: the daemon closes
// the connection once it replies.
func (c *Client) Do(req Request) (Response, error) {
	if c.timeout > 0 {
		if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
			return Response{}, fmt.Errorf("bus: set deadline: %w", err)
		}
		defer c.conn.SetDeadline(time.Time{})
	}

	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("bus: send request: %w", err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("bus: read response: %w", err)
	}
	if !resp.OK {
		return resp, responseError(resp)
	}
	return resp, nil
}

// Subscribe sends req and, once the daemon confirms with {"ok":true}, returns a
// channel of decoded Envelopes until the connection closes, Close is called, or
// a decode fails. The channel is always closed in one of those cases — never
// left open with nothing coming and no explanation. Subscribe consumes c for
// the life of the stream.
func (c *Client) Subscribe(req Request) (<-chan Envelope, error) {
	if c.timeout > 0 {
		if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
			return nil, fmt.Errorf("bus: set deadline: %w", err)
		}
	}

	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return nil, fmt.Errorf("bus: send subscribe request: %w", err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("bus: read subscribe response: %w", err)
	}
	if c.timeout > 0 {
		// The handshake is bounded like Do; the live stream that follows is not,
		// so the deadline is cleared the moment the handshake completes.
		if err := c.conn.SetDeadline(time.Time{}); err != nil {
			return nil, fmt.Errorf("bus: clear deadline: %w", err)
		}
	}
	if !resp.OK {
		return nil, responseError(resp)
	}

	ch := make(chan Envelope)
	go func() {
		defer close(ch)
		for {
			var env Envelope
			if err := c.dec.Decode(&env); err != nil {
				return
			}
			select {
			case ch <- env:
			case <-c.closed:
				// Close was called while this goroutine was parked handing off a
				// frame to a caller that stopped reading. Closing the connection
				// alone would not unblock a send on an unbuffered channel, so
				// without this case the goroutine leaks.
				return
			}
		}
	}()
	return ch, nil
}

// Close releases the connection and unblocks any Subscribe goroutine parked
// delivering to a channel nobody is reading. Safe to call more than once.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

// responseError converts a failed Response into an *Error, so a failure
// resolved on the daemon and one resolved locally on the client present the
// same shape to every call site.
func responseError(resp Response) error {
	return &Error{Code: resp.Code, Msg: resp.Error}
}

// maxSpawnAttempts bounds EnsureDaemon's spawn loop: an initial spawn plus one
// retry, never more. A daemon unreachable after a fresh spawn and one respawn
// will not come up on a third try, and retrying forever turns a crash loop
// into a hang instead of a clear failure.
const maxSpawnAttempts = 2

// Default timings for a production Ensurer, named here rather than hardcoded
// inline in DefaultEnsurer.
const (
	defaultDialTimeout  = 2 * time.Second
	defaultSpawnWait    = 5 * time.Second
	defaultPollInterval = 25 * time.Millisecond
)

// Ensurer holds the collaborators EnsureDaemon needs. Spawn is a function field
// so tests can substitute an in-process daemon goroutine instead of depending
// on a built `atomic` binary.
type Ensurer struct {
	// Spawn launches the daemon and returns; it does not wait for the socket to
	// come up, which EnsureDaemon polls for separately. The production default
	// shells out to `atomic bus serve` detached.
	Spawn func(home string) error

	// DialTimeout bounds each individual dial-and-ping attempt.
	DialTimeout time.Duration

	// SpawnWait bounds how long EnsureDaemon polls for the socket to start
	// accepting connections after Spawn returns.
	SpawnWait time.Duration

	// PollInterval spaces out that polling.
	PollInterval time.Duration
}

// DefaultEnsurer returns an Ensurer wired for production: Spawn shells out to
// `atomic bus serve` detached from the calling process.
func DefaultEnsurer() Ensurer {
	return Ensurer{
		Spawn:        spawnServe,
		DialTimeout:  defaultDialTimeout,
		SpawnWait:    defaultSpawnWait,
		PollInterval: defaultPollInterval,
	}
}

// EnsureDaemon calls DefaultEnsurer().EnsureDaemon(home).
func EnsureDaemon(home string) (*Client, error) {
	return DefaultEnsurer().EnsureDaemon(home)
}

// EnsureDaemon guarantees a live, version-matched daemon is listening at
// SocketPath(home) and returns a connected Client.
//
// The whole probe-unlink-spawn-wait-reprobe sequence runs under one exclusive
// flock on LockPath(home). Guarding only the spawn call would still let two
// callers both observe "down" before either spawns, and both spawn; only
// locking the whole decision closes that window. A caller that loses the race
// blocks, then finds the daemon already live on its own probe — which is what
// makes concurrent cold calls produce exactly one daemon.
func (e Ensurer) EnsureDaemon(home string) (*Client, error) {
	if err := EnsureDirs(home); err != nil {
		return nil, fmt.Errorf("bus: ensure state dirs: %w", err)
	}

	lock, err := acquireLock(LockPath(home))
	if err != nil {
		return nil, fmt.Errorf("bus: acquire spawn lock %s: %w", LockPath(home), err)
	}
	defer lock.unlock()

	if client, err := e.connectAndVerify(home); err == nil {
		return client, nil
	} else if skew := asVersionSkew(err); skew != nil {
		return nil, skew.busError()
	}

	var lastErr error
	for attempt := 0; attempt < maxSpawnAttempts; attempt++ {
		if err := unlinkStaleSocket(home); err != nil {
			return nil, err
		}
		if err := e.spawnAndWaitForSocket(home); err != nil {
			lastErr = err
			continue
		}
		client, err := e.connectAndVerify(home)
		if err == nil {
			return client, nil
		}
		if skew := asVersionSkew(err); skew != nil {
			return nil, skew.busError()
		}
		lastErr = err
	}

	// Two spawn attempts failed to produce a reachable, version-matched daemon.
	// Fail rather than loop hoping a third succeeds.
	return nil, &Error{
		Code: ExitUnreachable,
		Msg:  fmt.Sprintf("bus: daemon unreachable after %d spawn attempt(s): %v", maxSpawnAttempts, lastErr),
	}
}

// connectAndVerify dials, pings to check the daemon's ProtocolVersion, then
// dials a second fresh connection to hand back — the daemon closes a connection
// after each one-shot round trip, so the probe connection cannot be reused. A
// dial failure is a plain error the caller reads as "not up yet, go spawn"; a
// version mismatch is a *versionSkewError so the caller can refuse instead.
func (e Ensurer) connectAndVerify(home string) (*Client, error) {
	probe, err := Dial(home, e.DialTimeout)
	if err != nil {
		return nil, err
	}
	verifyErr := checkVersion(probe)
	probe.Close()
	if verifyErr != nil {
		return nil, verifyErr
	}
	return Dial(home, e.DialTimeout)
}

// checkVersion pings client and refuses on a ProtocolVersion mismatch rather
// than draining and restarting: another session may hold a live `recv`
// subscription that one client's upgrade must not silently kill. A live process
// on the other end of the socket is not proof it is a working daemon serving
// this protocol — only a well-formed ping reply with a matching version is.
func checkVersion(client *Client) error {
	resp, err := client.Do(Request{Op: OpPing})
	if err != nil {
		return fmt.Errorf("bus: ping: %w", err)
	}
	var payload struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return fmt.Errorf("bus: parse ping payload: %w", err)
	}
	if payload.Version != ProtocolVersion {
		return &versionSkewError{running: payload.Version, client: ProtocolVersion}
	}
	return nil
}

// versionSkewError marks checkVersion's refusal so EnsureDaemon can tell a
// protocol mismatch from an ordinary "not up yet" dial failure. Skew must never
// trigger a spawn: it would either fail (the old daemon still holds the socket)
// or orphan that daemon's live subscribers.
type versionSkewError struct {
	running int
	client  int
}

func (e *versionSkewError) Error() string {
	return fmt.Sprintf(
		"bus: protocol version mismatch: daemon is running v%d, this client is v%d; run `atomic bus restart` to retire the old daemon, then retry",
		e.running, e.client,
	)
}

// busError converts to the wire-compatible *Error once EnsureDaemon has decided
// this is the terminal answer.
func (e *versionSkewError) busError() *Error {
	return &Error{Code: ExitUnreachable, Msg: e.Error()}
}

// asVersionSkew reports whether err is (or wraps) a *versionSkewError.
func asVersionSkew(err error) *versionSkewError {
	var skew *versionSkewError
	if errors.As(err, &skew) {
		return skew
	}
	return nil
}

// unlinkStaleSocket removes a socket file left behind by a crashed daemon:
// net.Listen refuses to bind a path that already exists even when nothing is
// listening. Removing an absent path is a benign no-op, so this runs ahead of
// every spawn attempt rather than branching on which probe failure triggered it.
func unlinkStaleSocket(home string) error {
	path := SocketPath(home)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("bus: remove stale socket %s: %w", path, err)
	}
	return nil
}

// spawnAndWaitForSocket calls e.Spawn, then polls the socket until it accepts a
// connection or e.SpawnWait elapses — the bounded wait that keeps EnsureDaemon
// from reaching connectAndVerify before the daemon has bound its listener.
func (e Ensurer) spawnAndWaitForSocket(home string) error {
	if err := e.Spawn(home); err != nil {
		return fmt.Errorf("bus: spawn daemon: %w", err)
	}

	addr := SocketPath(home)
	deadline := time.Now().Add(e.SpawnWait)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", addr)
		if err == nil {
			conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(e.PollInterval)
	}
	return fmt.Errorf("bus: socket %s did not accept connections within %s of spawning: %w", addr, e.SpawnWait, lastErr)
}

// spawnServe launches `atomic bus serve` detached so the daemon outlives the CLI
// invocation that spawned it. Locating the binary via os.Executable rather than
// a PATH lookup guarantees the daemon is the same build as the client spawning
// it.
func spawnServe(home string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("bus: locate atomic binary: %w", err)
	}
	// Under `go test`, os.Executable is the <pkg>.test binary: it ignores these
	// arguments and re-runs the whole suite, whose tests call EnsureDaemon and
	// spawn again, multiplying until the machine is out of memory. The guard
	// lives in the production path rather than the tests because Ensurer.Spawn
	// is injectable and one call site forgetting to inject is enough to fork-bomb
	// the developer's machine.
	if isTestBinary(exe) {
		return fmt.Errorf("bus: refusing to spawn a daemon from test binary %s "+
			"(inject Ensurer.Spawn in tests)", filepath.Base(exe))
	}
	cmd := exec.Command(exe, "bus", "serve")
	cmd.Env = append(os.Environ(), "HOME="+home)
	// Setsid detaches the daemon from this invocation's controlling terminal and
	// process group, so it survives a signal meant for the parent and outlives
	// this process.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// isTestBinary reports whether this process is a compiled Go test binary. Both
// signals are unambiguous for a real `atomic` invocation: `go test` names the
// binary <pkg>.test and the testing package registers -test.* flags.
func isTestBinary(exe string) bool {
	if strings.HasSuffix(filepath.Base(exe), ".test") {
		return true
	}
	if len(os.Args) > 0 && strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		return true
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

// flockFile is a held exclusive lock on a file, released by unlock.
type flockFile struct {
	f *os.File
}

// acquireLock takes a blocking exclusive flock on path, creating the file if
// absent. Every call opens a fresh descriptor: POSIX flock locks are scoped to
// the open file description, not the path, so two callers racing for one path
// through separate os.OpenFile calls genuinely contend rather than each locking
// its own private view.
func acquireLock(path string) (*flockFile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("bus: open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("bus: flock %s: %w", path, err)
	}
	return &flockFile{f: f}, nil
}

func (l *flockFile) unlock() error {
	defer l.f.Close()
	return syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
}
