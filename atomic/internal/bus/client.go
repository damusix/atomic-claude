package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Client is one connection to the daemon's Unix domain socket
// (SocketPath). Do performs a single request/response round trip;
// Subscribe performs the same round trip and then streams Envelope frames
// until the connection closes; Close releases the underlying connection.
// Dial connects without any health check; EnsureDaemon is the guarded path
// that guarantees a live, version-matched daemon before handing back a
// Client.
//
// A Client is one-shot: good for exactly one Do call or one Subscribe
// call, because the daemon closes the connection after every non-
// subscription round trip (see connectAndVerify). Reusing a Client for a
// second call fails with a connection error, not a documented one.
type Client struct {
	conn net.Conn
	dec  *json.Decoder

	// timeout bounds Do's round trip and Subscribe's opening handshake —
	// never the live stream that follows a successful Subscribe, which
	// legitimately waits indefinitely for the next message. <= 0 disables
	// the deadline entirely.
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

// Dial opens a new connection to the daemon's socket at SocketPath(home),
// with no probe-and-spawn behavior of its own — callers that need the
// daemon guaranteed running should call EnsureDaemon instead.
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

// Do sends req and returns the daemon's Response. When the daemon replies
// {"ok":false}, Do also returns a non-nil error — checkpoint 1's *Error,
// carrying the same Code the daemon assigned (protocol.go's Response.Code
// doc: the daemon owns exit-code assignment). The client never re-derives
// an exit code from Error's message text. Do consumes c: the daemon closes
// the connection once it replies, so a second Do or Subscribe on the same
// Client fails.
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

// Subscribe sends req (an OpRecv with Follow set, or an OpTail) and, once
// the daemon confirms with its opening {"ok":true}, returns a channel that
// receives one decoded Envelope per frame until the connection closes,
// Close is called, or a decode error occurs. The channel is always closed
// in one of those cases — never left open with nothing coming and no
// explanation. Subscribe likewise consumes c for the life of the stream —
// do not call Do or Subscribe again on the same Client.
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
		// The opening handshake above is bounded like Do; the live stream
		// that follows is not — a subscriber legitimately waits
		// indefinitely for the next message, so the deadline is cleared
		// the moment the handshake completes.
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
				// Close was called while this goroutine was parked
				// trying to hand off a frame to a caller that stopped
				// reading — without this case, closing the connection
				// alone would not unblock a send on an unbuffered
				// channel, and this goroutine would leak.
				return
			}
		}
	}()
	return ch, nil
}

// Close releases the underlying connection and unblocks any goroutine
// Subscribe started that's parked delivering to a channel nobody is
// reading. Safe to call more than once.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

// responseError converts a failed Response into checkpoint 1's *Error
// type, so a failure resolved on the daemon and one resolved locally on
// the client (e.g. State.ResolveRoom's not-joined case) present the same
// shape to every call site.
func responseError(resp Response) error {
	return &Error{Code: resp.Code, Msg: resp.Error}
}

// maxSpawnAttempts bounds EnsureDaemon's spawn loop: an initial spawn plus
// exactly one retry on stale-socket recovery, never more. A daemon that
// still isn't reachable after a fresh spawn and one respawn is not going
// to come up on a third try either — retrying indefinitely would turn a
// crash loop into a hang instead of a clear, actionable failure.
const maxSpawnAttempts = 2

// Default timings for a production Ensurer. Exported as defaults (not
// hardcoded inline) so DefaultEnsurer's values are named and so tests can
// see what "production" means without re-deriving it.
const (
	defaultDialTimeout  = 2 * time.Second
	defaultSpawnWait    = 5 * time.Second
	defaultPollInterval = 25 * time.Millisecond
)

// Ensurer holds the collaborators EnsureDaemon needs. Spawn is a function
// field — the testable-seam convention documented in
// .claude/skills/atomic-cli-contrib/SKILL.md §2 — so tests substitute an
// in-process daemon goroutine instead of depending on a built `atomic`
// binary. DefaultEnsurer wires the production default; production wiring
// of the CLI dispatch itself (calling EnsureDaemon from `atomic bus join`
// etc.) is checkpoint 4's job.
type Ensurer struct {
	// Spawn starts the daemon and returns once the process has been
	// launched — it does not need to wait for the socket to come up;
	// EnsureDaemon polls for that separately (see SpawnWait/PollInterval
	// below). The production default shells out to `atomic bus serve`
	// detached.
	Spawn func(home string) error

	// DialTimeout bounds each individual dial-and-ping attempt.
	DialTimeout time.Duration

	// SpawnWait bounds how long EnsureDaemon polls for the socket to
	// start accepting connections after Spawn returns.
	SpawnWait time.Duration

	// PollInterval spaces out that polling.
	PollInterval time.Duration
}

// DefaultEnsurer returns an Ensurer wired for production: Spawn shells out
// to `atomic bus serve` detached from the calling process.
func DefaultEnsurer() Ensurer {
	return Ensurer{
		Spawn:        spawnServe,
		DialTimeout:  defaultDialTimeout,
		SpawnWait:    defaultSpawnWait,
		PollInterval: defaultPollInterval,
	}
}

// EnsureDaemon is a convenience wrapper calling
// DefaultEnsurer().EnsureDaemon(home) — the production entry point future
// CLI dispatch calls.
func EnsureDaemon(home string) (*Client, error) {
	return DefaultEnsurer().EnsureDaemon(home)
}

// EnsureDaemon guarantees a live, version-matched daemon is listening at
// SocketPath(home) and returns a connected Client to it.
//
// The entire probe-and-spawn sequence below — probe, unlink, spawn, wait,
// reprobe, retry — runs while holding one exclusive flock on
// LockPath(home). A check-then-spawn design (probe outside the lock, only
// guard the spawn call itself) would let two concurrent callers both
// observe "down" before either spawns, and both spawn: locking merely the
// spawn call does not close that window, only locking the whole decision
// does. Because of that, a caller that loses the race for the lock simply
// blocks; once it wakes, the daemon a prior caller already spawned is
// live, so its own probe succeeds and it never spawns a second one — this
// is what makes concurrent EnsureDaemon calls from cold produce exactly
// one daemon (see the marquee test in client_test.go).
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

	// Two consecutive spawn attempts failed to produce a reachable,
	// version-matched daemon. Per the brief: retry exactly once, then
	// fail — never loop indefinitely hoping a third attempt succeeds.
	return nil, &Error{
		Code: ExitUnreachable,
		Msg:  fmt.Sprintf("bus: daemon unreachable after %d spawn attempt(s): %v", maxSpawnAttempts, lastErr),
	}
}

// connectAndVerify dials SocketPath(home), pings it to check the daemon's
// ProtocolVersion, then dials a second, fresh connection to hand back —
// the daemon closes a connection after each one-shot round trip (see
// daemon.go's handleConn), so the probe connection the ping consumed
// cannot also be handed back for the caller's own use. A dial failure (no
// socket, or connection refused against a stale one) is returned as a
// plain error — the caller treats that as "not up yet, go spawn". A
// version mismatch is wrapped as *versionSkewError so the caller can tell
// it apart and refuse instead of spawning (see checkVersion's doc).
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

// checkVersion pings client and refuses on a ProtocolVersion mismatch —
// see docs/design/atomic-bus.md's "Resolved open decisions" #2: refuse
// rather than drain-and-restart, because another session may be holding a
// live `recv --follow` subscription that one client's upgrade must not
// silently kill. Health is judged strictly by this round trip: a
// live process at the other end of the socket is not, on its own, proof
// that process is a working daemon serving this protocol — only a
// well-formed ping reply with a matching version is.
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
// protocol mismatch apart from an ordinary "daemon not up yet" dial
// failure: skew must refuse outright and must never trigger a
// spawn/respawn attempt — spawning here would either fail (the old daemon
// still holds the socket) or, worse, orphan the old daemon's live
// subscribers if it were ever taught to drain instead.
type versionSkewError struct {
	running int
	client  int
}

func (e *versionSkewError) Error() string {
	return fmt.Sprintf(
		"bus: protocol version mismatch: daemon is running v%d, this client is v%d; run `atomic bus serve --stop` to retire the old daemon, then retry",
		e.running, e.client,
	)
}

// busError converts to the wire-compatible *Error (ExitUnreachable, exit
// code 6) once EnsureDaemon has decided this is the terminal answer.
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

// unlinkStaleSocket removes home's socket file if present — the
// stale-socket recovery step. A crashed daemon leaves the file behind,
// and net.Listen refuses to bind a path that already exists even when
// nothing is listening on it; os.Remove on an absent path is a benign
// no-op, so this runs unconditionally ahead of every spawn attempt rather
// than branching on which probe failure triggered it.
func unlinkStaleSocket(home string) error {
	path := SocketPath(home)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("bus: remove stale socket %s: %w", path, err)
	}
	return nil
}

// spawnAndWaitForSocket calls e.Spawn, then polls SocketPath(home) until
// it accepts a raw connection or e.SpawnWait elapses. Spawn itself only
// launches the daemon; this is the bounded wait that keeps EnsureDaemon
// from moving on to connectAndVerify before the daemon has had a chance to
// bind its listener.
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

// spawnServe is Ensurer's production Spawn: it launches `atomic bus serve`
// detached from this process so the daemon outlives the CLI invocation
// that spawned it. Locating the atomic binary via os.Executable (rather
// than a bare "atomic" resolved against PATH) guarantees the spawned
// daemon is the same build as the client deciding to spawn it.
func spawnServe(home string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("bus: locate atomic binary: %w", err)
	}
	cmd := exec.Command(exe, "bus", "serve")
	cmd.Env = append(os.Environ(), "HOME="+home)
	// Setsid starts the daemon in its own session, detached from this
	// invocation's controlling terminal and process group — so it isn't
	// killed by a signal meant for the parent (e.g. the terminal closing)
	// and survives after this CLI invocation exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// flockFile is a held exclusive lock on a file, released by unlock.
type flockFile struct {
	f *os.File
}

// acquireLock takes a blocking, exclusive flock on path, creating the
// file if absent. EnsureDaemon holds this for its entire probe-and-spawn
// sequence — see that function's doc for why the lock must span the whole
// sequence rather than guarding only the spawn call. Every call opens a
// fresh file descriptor: POSIX flock locks are scoped to the open file
// description, not merely the path, so two callers racing for the same
// path via two separate os.OpenFile calls genuinely contend rather than
// each locking (and unlocking) their own private view of the file.
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
