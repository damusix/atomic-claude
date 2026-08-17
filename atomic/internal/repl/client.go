package repl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var (
	// ErrSessionNotFound is no socket at the path — never started, or reaped.
	// Deliberately one answer for both: the remedy is `start` either way, and a
	// marker separating them would invite branching on unactionable history.
	ErrSessionNotFound = errors.New("repl: session not found")

	// ErrSessionDead is a socket that exists but refuses. Reported rather than
	// silently restarted — a restart would hide that the session's state is gone.
	ErrSessionDead = errors.New("repl: session dead")

	// ErrEvalTimeout is the eval deadline elapsing. It always means the
	// session has been ended and removed; see Eval.
	ErrEvalTimeout = errors.New("repl: eval timed out")
)

// ProtocolMismatchError is a response from a harness speaking a version this
// binary does not. Its own type because the fix is specific and nothing else
// will do: the harness predates an `atomic update` and has to be replaced.
type ProtocolMismatchError struct {
	Harness int
	Client  int
}

func (e *ProtocolMismatchError) Error() string {
	return fmt.Sprintf(
		"repl: protocol version mismatch: the running harness speaks v%d, this binary speaks v%d; run `atomic repl stop` then `atomic repl start` to replace the session",
		e.Harness, e.Client)
}

const (
	// DefaultEvalTimeout bounds one eval's wait for a response.
	DefaultEvalTimeout = 30 * time.Second
	// DefaultEvalGrace is how long the escalation waits after SIGINT before
	// reaching for SIGKILL.
	DefaultEvalGrace = 2 * time.Second
	// defaultGracePoll is how often that wait re-checks the process.
	defaultGracePoll = 25 * time.Millisecond
	// defaultDialTimeout bounds the connect. A unix connect either succeeds
	// immediately or fails; this only guards a pathological peer.
	defaultDialTimeout = 5 * time.Second
	// pidStartTolerance is the slack between a session's recorded start and the
	// start time the OS reports for its pid: enough to absorb elapsed-time's
	// one-second granularity, far tighter than any realistic pid-reuse interval.
	pidStartTolerance = 5 * time.Second
)

// SignalFunc delivers sig to pid. Injectable so the escalation is testable
// without a test ever signaling a real process.
type SignalFunc func(pid int, sig os.Signal) error

// PidMatchFunc reports whether pid names a live process that started at
// startedAt — the recycled-pid guard. It doubles as the liveness probe during
// the grace period, so the escalation never escalates at a process whose
// identity it did not just re-verify.
type PidMatchFunc func(pid int, startedAt time.Time) bool

// Session is one session's on-disk identity plus the meta the escalation needs.
type Session struct {
	SocketPath string
	MetaPath   string
	Meta       Meta
}

// Client is one connection to a session's harness. A harness answers one request
// per connection and then closes, so a Client is good for exactly one Do.
type Client struct {
	conn    net.Conn
	reader  *bufio.Reader
	timeout time.Duration
}

// Dial connects to a session's socket, classifying the two failures that mean
// different things: an absent socket is a session that is not there, a refused
// one is a session that died without cleaning up.
func Dial(socketPath string, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	conn, err := net.DialTimeout("unix", socketPath, timeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, socketPath)
		}
		return nil, fmt.Errorf("%w: %s: %w", ErrSessionDead, socketPath, err)
	}
	return &Client{conn: conn, reader: bufio.NewReader(conn), timeout: timeout}, nil
}

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Do performs one newline-delimited-JSON round trip. A response carrying a
// version this binary does not speak is refused rather than decoded: the fields
// it does parse may not mean what they appear to.
func (c *Client) Do(req Request) (Response, error) {
	if c.timeout > 0 {
		if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
			return Response{}, fmt.Errorf("repl: set deadline: %w", err)
		}
	}

	frame, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("repl: encode request: %w", err)
	}
	if _, err := c.conn.Write(append(frame, '\n')); err != nil {
		return Response{}, fmt.Errorf("repl: send request: %w", err)
	}

	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return Response{}, fmt.Errorf("repl: read response: %w", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("repl: decode response: %w", err)
	}
	if resp.V != ProtocolVersion {
		return Response{}, &ProtocolMismatchError{Harness: resp.V, Client: ProtocolVersion}
	}
	return resp, nil
}

// EvalOptions bounds one eval and shapes what happens when that bound is hit.
// Every zero field takes a documented default; Signal and PidMatch are seams so
// the escalation runs without a real process or a real wait.
type EvalOptions struct {
	Timeout   time.Duration
	Grace     time.Duration
	GracePoll time.Duration

	Signal   SignalFunc
	PidMatch PidMatchFunc
}

// Eval runs code against sess and returns the harness's response.
//
// An eval failure (ok=false with a traceback) comes back as a Response with a
// nil error: the command worked, the code did not, and collapsing the two would
// cost the caller the distinction its exit codes are built on.
//
// When the deadline elapses the session is ended, not merely abandoned (see
// escalate), so a caller receiving ErrEvalTimeout can rely on it being gone.
func Eval(sess Session, code string, opts EvalOptions) (Response, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultEvalTimeout
	}

	client, err := Dial(sess.SocketPath, defaultDialTimeout)
	if err != nil {
		return Response{}, err
	}
	defer client.Close()

	client.timeout = timeout
	resp, err := client.Do(Request{V: ProtocolVersion, Op: OpEval, Code: code})
	if err == nil {
		return resp, nil
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		return Response{}, err
	}
	return Response{}, opts.escalate(sess)
}

// escalate ends a session that blew its deadline, always reporting
// ErrEvalTimeout.
//
// The two harnesses answer SIGINT differently: Node installs no handler, so the
// default disposition kills it outright even mid-eval, while Python catches the
// KeyboardInterrupt inside the eval and keeps serving. Waiting for a graceful
// answer would therefore leave Python alive holding whatever the runaway eval
// did to its namespace and Node dead with its files still on disk — two
// outcomes for one command. So the escalation converges them: SIGINT, a grace
// period, SIGKILL if anything remains, and the files removed either way.
//
// Nothing is signaled unless the pid's identity verifies first. A pid read from
// a file is a number, not a process, and a SIGKILL at the wrong one is
// unrecoverable.
func (o EvalOptions) escalate(sess Session) error {
	// Removal runs whatever the signaling path decides, including declining to
	// signal at all.
	defer removeSessionFiles(sess)

	pid := sess.Meta.PID
	if pid <= 0 {
		// No pid to verify. Signaling 0 would reach this process's whole group,
		// and a negative pid a whole other one.
		return ErrEvalTimeout
	}

	signal := o.Signal
	if signal == nil {
		signal = defaultSignal
	}
	pidMatch := o.PidMatch
	if pidMatch == nil {
		pidMatch = defaultPidMatch
	}

	if !pidMatch(pid, sess.Meta.StartedAt) {
		// Already gone, or the number belongs to something else now. Either way
		// this session is over and nothing may be signaled.
		return ErrEvalTimeout
	}

	_ = signal(pid, syscall.SIGINT)

	grace := o.Grace
	if grace <= 0 {
		grace = DefaultEvalGrace
	}
	poll := o.GracePoll
	if poll <= 0 {
		poll = defaultGracePoll
	}
	deadline := time.Now().Add(grace)
	for {
		if !pidMatch(pid, sess.Meta.StartedAt) {
			return ErrEvalTimeout // the interrupt was enough
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(poll)
	}

	_ = signal(pid, syscall.SIGKILL)
	return ErrEvalTimeout
}

// removeSessionFiles clears the socket and meta so later verbs report not found.
// Absent files are expected when the harness cleaned up after itself first.
func removeSessionFiles(sess Session) {
	for _, path := range []string{sess.SocketPath, sess.MetaPath} {
		if path == "" {
			continue
		}
		_ = os.Remove(path)
	}
}

func defaultSignal(pid int, sig os.Signal) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	// By pid, not terminal job control: the harness is detached (Setsid), so it
	// has no controlling terminal to interrupt.
	return proc.Signal(sig)
}

// defaultPidMatch reports whether pid is a live process that started when this
// session's meta says it did.
//
// Elapsed time via ps rather than a start timestamp is what is portable across
// macOS and Linux: `etime` is POSIX and locale-independent, `lstart` is neither.
// A pid that no longer exists makes ps exit non-zero, which is the answer this
// needs anyway. A zombie reports as not matching: ps still lists an unreaped
// process with its elapsed time intact, so etime alone would call a corpse live
// — and callers use this as their liveness probe, not only as an identity check.
func defaultPidMatch(pid int, startedAt time.Time) bool {
	if pid <= 0 || startedAt.IsZero() {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "etime=,state=").Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return false
	}
	if strings.HasPrefix(fields[1], "Z") {
		return false
	}
	elapsed, err := parseETime(fields[0])
	if err != nil {
		return false
	}

	skew := time.Since(startedAt) - elapsed
	if skew < 0 {
		skew = -skew
	}
	return skew <= pidStartTolerance
}

// parseETime parses the POSIX elapsed-time format ps prints: [[dd-]hh:]mm:ss,
// with a bare ss for a process younger than a minute.
func parseETime(field string) (time.Duration, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, errors.New("repl: empty elapsed time")
	}

	var days int
	if dash := strings.IndexByte(field, '-'); dash >= 0 {
		parsed, err := strconv.Atoi(field[:dash])
		if err != nil {
			return 0, fmt.Errorf("repl: parse elapsed days %q: %w", field, err)
		}
		days = parsed
		field = field[dash+1:]
	}

	parts := strings.Split(field, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("repl: unrecognized elapsed time %q", field)
	}
	// Right-align: the last part is always seconds, whatever precedes it.
	units := []time.Duration{time.Second, time.Minute, time.Hour}
	total := time.Duration(days) * 24 * time.Hour
	for i := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1-i]))
		if err != nil {
			return 0, fmt.Errorf("repl: parse elapsed time %q: %w", field, err)
		}
		total += time.Duration(value) * units[i]
	}
	return total, nil
}
