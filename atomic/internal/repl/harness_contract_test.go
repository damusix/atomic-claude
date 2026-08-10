package repl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// This file holds the cross-language conformance suite the two harness tests
// share, so the Python and Node emitters are held to one contract rather than
// two hand-written approximations of it. harness_python_test.go and
// harness_node_test.go supply only their language's snippets.
//
// Every round trip here strict-decodes the harness's live JSON into the
// canonical Response — unknown fields rejected, exact key set asserted — which
// is what makes it impossible for one emitter to quietly drift from the other
// or from protocol.go.

const (
	// Long enough that no conformance subtest can trip the idle watchdog while
	// it runs. The self-exit path gets its own short-window harness.
	conformanceIdleTimeout = "60"

	// Bounds every wait in this file. A harness that has not come up, answered,
	// or exited by now is broken, and hanging the suite hides that.
	harnessBootTimeout = 20 * time.Second
	harnessCallTimeout = 20 * time.Second
	harnessExitTimeout = 20 * time.Second

	// The idle-window subtests trade a short real wait for the only honest
	// proof that the watchdog fires. idleProbeDeadline is generous against a
	// loaded machine but still finite — the failure it guards is a harness
	// that never exits at all, not one that exits late.
	idleProbeWindow   = "0.5"
	idleProbeInterval = 50 * time.Millisecond
	idleProbeDeadline = 5 * time.Second
)

// harnessCase is one language's instantiation of the shared suite.
type harnessCase struct {
	lang string // LangPython or LangNode
	bin  string // interpreter name resolved through exec.LookPath

	valueExpr string // final expression evaluates to 42
	multiline string // several statements, final expression evaluates to 42
	statement string // no final expression, so no value
	wantValue string // repr/inspect of 42 in this language

	stdoutCode string // writes "out-here\n" to stdout
	stderrCode string // writes "err-here\n" to stderr

	failCode         string   // logs "before-failure", then raises on line 2
	failMessage      string   // appears in the traceback
	failLineText     string   // the failing source line, verbatim
	failLineRef      string   // how this language cites line 2
	forbiddenInError []string // harness-internal frames that must be trimmed

	bigOutput   string // writes more than MaxStreamBytes to stdout
	smallOutput string // writes well under the cap
	surrogate   string // writes a lone surrogate to stdout
	// bigValue evaluates to a value whose repr/inspect exceeds MaxStreamBytes.
	// The two snippets differ in kind, not just syntax: Python's repr of a long
	// string is the whole string, while Node's util.inspect elides one past
	// maxStringLength (10000 by default) with a self-describing "... N more
	// characters", so only a large structure gets Node past the cap.
	bigValue string

	stateSet         string // binds a variable
	stateGet         string // reads it back, evaluating to 42
	resetErrorMarker string // error naming the unbound variable after a reset

	slowEval string // blocks for slowEvalWindow, then binds the marker fastEval reads and yields 'slow-done'
	fastEval string // computes 42 from slowEval's marker — unbound (an error) unless slowEval completed first
	wantFast string
}

// slowEvalWindow is how long each language's slowEval blocks. Long enough that
// a non-serializing harness would visibly finish the fast eval first, short
// enough not to drag the suite.
const slowEvalWindow = 600 * time.Millisecond

func runHarnessConformance(t *testing.T, c harnessCase) {
	t.Helper()

	t.Run("eval single line", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.valueExpr)
		assertOK(t, resp)
		if resp.Value != c.wantValue {
			t.Errorf("value = %q, want %q", resp.Value, c.wantValue)
		}
		if resp.Stdout != "" || resp.Stderr != "" {
			t.Errorf("stdout = %q, stderr = %q, want both empty", resp.Stdout, resp.Stderr)
		}
		if resp.Truncated {
			t.Error("truncated = true for output well under the cap")
		}
	})

	t.Run("eval multiline is one unit", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.multiline)
		assertOK(t, resp)
		if resp.Value != c.wantValue {
			t.Errorf("value = %q, want %q", resp.Value, c.wantValue)
		}
	})

	t.Run("bare statement has no value", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.statement)
		assertOK(t, resp)
		if resp.Value != "" {
			t.Errorf("value = %q, want \"\" for a statement", resp.Value)
		}
	})

	t.Run("captures stdout and stderr", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)

		out := h.eval(t, c.stdoutCode)
		assertOK(t, out)
		if out.Stdout != "out-here\n" {
			t.Errorf("stdout = %q, want %q", out.Stdout, "out-here\n")
		}
		if out.Stderr != "" {
			t.Errorf("stderr = %q, want empty", out.Stderr)
		}

		errOut := h.eval(t, c.stderrCode)
		assertOK(t, errOut)
		if errOut.Stderr != "err-here\n" {
			t.Errorf("stderr = %q, want %q", errOut.Stderr, "err-here\n")
		}
		if errOut.Stdout != "" {
			t.Errorf("stdout = %q, want empty", errOut.Stdout)
		}
	})

	t.Run("exception yields a traceback and pre-failure output", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.failCode)

		if resp.OK {
			t.Fatalf("ok = true for code that raised; response = %+v", resp)
		}
		if resp.Value != "" {
			t.Errorf("value = %q, want \"\" on failure", resp.Value)
		}
		if resp.Stdout != "before-failure\n" {
			t.Errorf("stdout = %q, want %q — output produced before the failure is still delivered",
				resp.Stdout, "before-failure\n")
		}
		for _, want := range []string{c.failMessage, c.failLineText, c.failLineRef} {
			if !strings.Contains(resp.Error, want) {
				t.Errorf("error does not contain %q; got:\n%s", want, resp.Error)
			}
		}
		for _, forbidden := range c.forbiddenInError {
			if strings.Contains(resp.Error, forbidden) {
				t.Errorf("error leaks harness frame %q; got:\n%s", forbidden, resp.Error)
			}
		}
	})

	t.Run("the session survives an eval exception", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		h.eval(t, c.failCode)
		resp := h.eval(t, c.valueExpr)
		assertOK(t, resp)
		if resp.Value != c.wantValue {
			t.Errorf("value = %q, want %q after a prior failure", resp.Value, c.wantValue)
		}
	})

	t.Run("stdout truncated at the cap", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.bigOutput)
		assertOK(t, resp)
		if !resp.Truncated {
			t.Error("truncated = false for output over the cap")
		}
		if len(resp.Stdout) > MaxStreamBytes {
			t.Errorf("stdout is %d bytes, over the %d-byte cap", len(resp.Stdout), MaxStreamBytes)
		}
		// A cut can drop at most one partial code point, never a meaningful
		// slice of the payload.
		if len(resp.Stdout) < MaxStreamBytes-4 {
			t.Errorf("stdout is %d bytes, want ~%d — truncation cut too much", len(resp.Stdout), MaxStreamBytes)
		}
		if !utf8.ValidString(resp.Stdout) {
			t.Error("truncated stdout is not valid UTF-8")
		}
	})

	t.Run("oversized value is truncated", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.bigValue)
		assertOK(t, resp)
		// The cap covers the value, not just the streams: a repr of a large
		// object is as unbounded as a runaway print loop, and either one can
		// hand the client a frame it has to buffer whole.
		if !resp.Truncated {
			t.Error("truncated = false for a value over the cap")
		}
		if len(resp.Value) > MaxStreamBytes {
			t.Errorf("value is %d bytes, over the %d-byte cap", len(resp.Value), MaxStreamBytes)
		}
		if len(resp.Value) < MaxStreamBytes-4 {
			t.Errorf("value is %d bytes, want ~%d — truncation cut too much", len(resp.Value), MaxStreamBytes)
		}
		if !utf8.ValidString(resp.Value) {
			t.Error("truncated value is not valid UTF-8")
		}
		if resp.Stdout != "" {
			t.Errorf("stdout = %q, want empty — the cap fired on the value, not on output", resp.Stdout)
		}
	})

	t.Run("output under the cap is delivered whole", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.smallOutput)
		assertOK(t, resp)
		if resp.Truncated {
			t.Error("truncated = true for output under the cap")
		}
		if resp.Stdout != "short\n" {
			t.Errorf("stdout = %q, want %q", resp.Stdout, "short\n")
		}
	})

	t.Run("output is sanitized to valid UTF-8", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.eval(t, c.surrogate)
		assertOK(t, resp)
		if !utf8.ValidString(resp.Stdout) {
			t.Errorf("stdout %q is not valid UTF-8", resp.Stdout)
		}
		if !strings.HasPrefix(resp.Stdout, "a") || !strings.HasSuffix(resp.Stdout, "b") {
			t.Errorf("stdout = %q, want the surrounding a/b preserved with the surrogate replaced", resp.Stdout)
		}
	})

	t.Run("state persists across connections", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		assertOK(t, h.eval(t, c.stateSet))
		// A second, separate connection — the same shape the CLI uses across
		// two Bash calls.
		resp := h.eval(t, c.stateGet)
		assertOK(t, resp)
		if resp.Value != c.wantValue {
			t.Errorf("value = %q, want %q — state did not survive the connection", resp.Value, c.wantValue)
		}
	})

	t.Run("reset clears the namespace and keeps the process", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		assertOK(t, h.eval(t, c.stateSet))
		assertOK(t, h.do(t, Request{V: ProtocolVersion, Op: OpReset}))

		resp := h.eval(t, c.stateGet)
		if resp.OK {
			t.Fatalf("ok = true reading a variable reset should have cleared; value = %q", resp.Value)
		}
		if !strings.Contains(resp.Error, c.resetErrorMarker) {
			t.Errorf("error does not contain %q; got:\n%s", c.resetErrorMarker, resp.Error)
		}
		// The process is still serving: a fresh binding works.
		assertOK(t, h.eval(t, c.stateSet))
		assertOK(t, h.eval(t, c.stateGet))
	})

	t.Run("ping", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.do(t, Request{V: ProtocolVersion, Op: OpPing})
		assertOK(t, resp)
		if resp.Stdout != "" || resp.Stderr != "" || resp.Value != "" || resp.Error != "" || resp.Truncated {
			t.Errorf("ping response carries payload: %+v", resp)
		}
	})

	t.Run("protocol version mismatch fails loud", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.do(t, Request{V: ProtocolVersion + 99, Op: OpPing})
		if resp.OK {
			t.Fatal("ok = true for a mismatched protocol version")
		}
		if resp.V != ProtocolVersion {
			t.Errorf("response v = %d, want the harness's own %d so the client can name the skew", resp.V, ProtocolVersion)
		}
		for _, want := range []string{"version", "atomic repl stop", "atomic repl start"} {
			if !strings.Contains(resp.Error, want) {
				t.Errorf("error does not contain %q; got: %s", want, resp.Error)
			}
		}
	})

	t.Run("unknown op names the valid ops", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		resp := h.do(t, Request{V: ProtocolVersion, Op: "bogus"})
		if resp.OK {
			t.Fatal("ok = true for an unknown op")
		}
		for _, op := range AllOps {
			if !strings.Contains(resp.Error, op) {
				t.Errorf("error does not name valid op %q; got: %s", op, resp.Error)
			}
		}
	})

	t.Run("malformed request does not end the session", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		conn := h.dial(t)
		conn.writeRaw(t, "this is not json\n")
		resp, _ := conn.read(t)
		if resp.OK {
			t.Fatal("ok = true for a malformed request")
		}
		if !strings.Contains(resp.Error, "malformed request") {
			t.Errorf("error = %q, want it to name the malformed request", resp.Error)
		}
		assertOK(t, h.eval(t, c.valueExpr))
	})

	t.Run("concurrent evals serialize", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)

		slowDone := make(chan Response, 1)
		fastDone := make(chan Response, 1)

		slowConn := h.dial(t)
		fastConn := h.dial(t)

		go func() {
			slowConn.write(t, Request{V: ProtocolVersion, Op: OpEval, Code: c.slowEval})
			resp, _ := slowConn.read(t)
			slowDone <- resp
		}()

		// Give the harness time to accept the slow connection and start
		// executing before the second one is offered, so this really tests
		// serialization rather than accept ordering.
		time.Sleep(slowEvalWindow / 6)

		go func() {
			fastConn.write(t, Request{V: ProtocolVersion, Op: OpEval, Code: c.fastEval})
			resp, _ := fastConn.read(t)
			fastDone <- resp
		}()

		slow := waitOutcome(t, slowDone, "slow eval")
		fast := waitOutcome(t, fastDone, "fast eval")

		assertOK(t, slow)
		if fast.Error != "" {
			t.Errorf("second eval errored while another was in flight: %s", fast.Error)
		}
		assertOK(t, fast)
		// Ordering is proven inside the interpreter: fastEval computes its
		// value from a marker slowEval binds as its final act, so a harness
		// that ran them concurrently yields an unbound-name error or a wrong
		// value here. Comparing client-side time.Now() stamps instead was
		// flaky — both responses can arrive within the same instant and
		// goroutine scheduling then decides which stamps first.
		if fast.Value != c.wantFast {
			t.Errorf("second eval value = %q, want %q — evals did not serialize", fast.Value, c.wantFast)
		}
	})

	t.Run("shutdown exits 0 and removes socket and meta", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		assertOK(t, h.do(t, Request{V: ProtocolVersion, Op: OpShutdown}))
		h.assertCleanExit(t)
	})

	t.Run("idle timeout self-exits and cleans up", func(t *testing.T) {
		h := startHarness(t, c, idleProbeWindow)
		// The window is measured from the last request, not from boot.
		assertOK(t, h.do(t, Request{V: ProtocolVersion, Op: OpPing}))
		h.assertCleanExit(t)
	})

	t.Run("idle timer counts from the last answered request", func(t *testing.T) {
		h := startHarness(t, c, idleProbeWindow)
		assertOK(t, h.do(t, Request{V: ProtocolVersion, Op: OpPing}))

		// Connect and hang up without asking anything, faster than the idle
		// window, for as long as this subtest runs. A harness that bumps its
		// clock per accepted connection rather than per answered request
		// stays alive forever under this traffic — which is the whole point:
		// the two harnesses reached the same behavior by different routes
		// (Python once bumped after every accept), so the contract has to be
		// asserted rather than assumed.
		stop := make(chan struct{})
		probing := make(chan struct{})
		go func() {
			defer close(probing)
			for {
				select {
				case <-stop:
					return
				case <-time.After(idleProbeInterval):
					if conn, err := net.Dial("unix", h.socketPath); err == nil {
						conn.Close()
					}
				}
			}
		}()
		t.Cleanup(func() {
			close(stop)
			<-probing
		})

		h.assertCleanExitWithin(t, idleProbeDeadline)
	})

	t.Run("the socket is created 0600", func(t *testing.T) {
		h := startHarness(t, c, conformanceIdleTimeout)
		info, err := os.Stat(h.socketPath)
		if err != nil {
			t.Fatalf("stat socket: %v", err)
		}
		// A session socket is code execution into a process that may hold
		// --env secrets, so it is never left at the house default. Each
		// harness now also sets its process umask to 0o177 before bind (born
		// 0600, not just chmod'd to it afterward) — not independently
		// asserted here: the window that hardening closes is between bind and
		// the very next chmod syscall inside the same process, too narrow for
		// this out-of-process stat to race reliably, so this subtest still
		// only proves the end state both mechanisms agree on.
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("socket mode = %o, want 600", perm)
		}
	})
}

// -- driver ------------------------------------------------------------------

type harnessProcess struct {
	socketPath string
	metaPath   string
	cmd        *exec.Cmd
	stderr     *syncBuffer
	exited     chan struct{}
	waitErr    error // read only after exited is closed
}

// syncBuffer collects a harness's stderr. os/exec copies into it from its own
// goroutine while the failure paths here read it to report what went wrong, so
// the buffer has to be safe for both at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startHarness spawns c's interpreter directly against the embedded harness
// script, skipping the test when that interpreter is not installed. The
// process is killed on cleanup whether or not the test got that far.
func startHarness(t *testing.T, c harnessCase, idleTimeout string) *harnessProcess {
	t.Helper()

	bin, err := exec.LookPath(c.bin)
	if err != nil {
		t.Skipf("%s not on PATH: %v", c.bin, err)
	}

	dir := shortTempDir(t)
	script, err := HarnessScript(c.lang)
	if err != nil {
		t.Fatalf("HarnessScript(%q): %v", c.lang, err)
	}
	name, err := HarnessFilename(c.lang)
	if err != nil {
		t.Fatalf("HarnessFilename(%q): %v", c.lang, err)
	}
	scriptPath := filepath.Join(dir, name)
	if err := os.WriteFile(scriptPath, script, 0o700); err != nil {
		t.Fatalf("write harness script: %v", err)
	}

	h := &harnessProcess{
		socketPath: filepath.Join(dir, "s.sock"),
		metaPath:   filepath.Join(dir, "s.meta.json"),
		stderr:     &syncBuffer{},
		exited:     make(chan struct{}),
	}
	if err := os.WriteFile(h.metaPath, []byte(`{"name":"s"}`), 0o600); err != nil {
		t.Fatalf("write meta file: %v", err)
	}

	h.cmd = exec.Command(bin, scriptPath,
		"--socket", h.socketPath,
		"--idle-timeout", idleTimeout,
		"--meta", h.metaPath)
	h.cmd.Stderr = h.stderr
	if err := h.cmd.Start(); err != nil {
		t.Fatalf("start harness: %v", err)
	}
	go func() {
		h.waitErr = h.cmd.Wait()
		close(h.exited)
	}()

	t.Cleanup(func() {
		select {
		case <-h.exited:
			return
		default:
		}
		_ = h.cmd.Process.Kill()
		select {
		case <-h.exited:
		case <-time.After(harnessExitTimeout):
			t.Errorf("harness did not exit after being killed")
		}
	})

	h.waitForSocket(t)
	return h
}

func (h *harnessProcess) waitForSocket(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(harnessBootTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-h.exited:
			t.Fatalf("harness exited before binding its socket: %v\nstderr:\n%s", h.waitErr, h.stderr)
		default:
		}
		conn, err := net.Dial("unix", h.socketPath)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("harness socket %s never accepted a connection within %s\nstderr:\n%s",
		h.socketPath, harnessBootTimeout, h.stderr)
}

// assertCleanExit waits for the harness to end on its own — shutdown or the
// idle watchdog — and asserts it exited 0 having removed both of its files.
func (h *harnessProcess) assertCleanExit(t *testing.T) {
	t.Helper()
	h.assertCleanExitWithin(t, harnessExitTimeout)
}

// assertCleanExitWithin is assertCleanExit under a caller-chosen bound, for the
// idle subtests whose whole assertion is that the exit happens promptly.
func (h *harnessProcess) assertCleanExitWithin(t *testing.T, within time.Duration) {
	t.Helper()
	select {
	case <-h.exited:
	case <-time.After(within):
		t.Fatalf("harness still running after %s\nstderr:\n%s", within, h.stderr)
	}
	if h.waitErr != nil {
		t.Errorf("harness exited with %v, want 0\nstderr:\n%s", h.waitErr, h.stderr)
	}
	for _, path := range []string{h.socketPath, h.metaPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists after exit (stat err %v); the harness cleans up after itself", path, err)
		}
	}
}

func (h *harnessProcess) eval(t *testing.T, code string) Response {
	t.Helper()
	return h.do(t, Request{V: ProtocolVersion, Op: OpEval, Code: code})
}

// do runs one request over its own connection, mirroring the CLI's one-shot
// round trip.
func (h *harnessProcess) do(t *testing.T, req Request) Response {
	t.Helper()
	conn := h.dial(t)
	conn.write(t, req)
	resp, _ := conn.read(t)
	return resp
}

func (h *harnessProcess) dial(t *testing.T) *harnessConn {
	t.Helper()
	conn, err := net.Dial("unix", h.socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v\nstderr:\n%s", h.socketPath, err, h.stderr)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(harnessCallTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	return &harnessConn{conn: conn, reader: bufio.NewReader(conn), owner: h}
}

type harnessConn struct {
	conn   net.Conn
	reader *bufio.Reader
	owner  *harnessProcess
}

func (c *harnessConn) write(t *testing.T, req Request) {
	t.Helper()
	frame, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	c.writeRaw(t, string(frame)+"\n")
}

func (c *harnessConn) writeRaw(t *testing.T, frame string) {
	t.Helper()
	if _, err := c.conn.Write([]byte(frame)); err != nil {
		t.Fatalf("write request: %v", err)
	}
}

// read decodes one response frame strictly: unknown fields are rejected and the
// raw key set must be exactly the seven the protocol documents. That is the
// cross-language contract — a harness that renames, drops, or adds a field
// fails here rather than degrading silently on the client.
func (c *harnessConn) read(t *testing.T) (Response, []byte) {
	t.Helper()
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read response: %v\nstderr:\n%s", err, c.owner.stderr)
	}

	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(line, &keyed); err != nil {
		t.Fatalf("response is not a JSON object: %v\nframe: %s", err, line)
	}
	assertExactKeys(t, keyed, responseWireKeys)

	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var resp Response
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response into the canonical Go struct: %v\nframe: %s", err, line)
	}
	if resp.V != ProtocolVersion {
		t.Errorf("response v = %d, want %d", resp.V, ProtocolVersion)
	}
	return resp, line
}

func assertOK(t *testing.T, resp Response) {
	t.Helper()
	if !resp.OK {
		t.Fatalf("ok = false: error = %s stderr = %q", resp.Error, resp.Stderr)
	}
	if resp.Error != "" {
		t.Errorf("error = %q on a successful response, want empty", resp.Error)
	}
}

func waitOutcome[T any](t *testing.T, ch <-chan T, label string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(harnessCallTimeout):
		var zero T
		t.Fatalf("%s never completed within %s", label, harnessCallTimeout)
		return zero
	}
}

// shortTempDir keeps the socket path inside the ~104-byte sun_path limit: on
// macOS $TMPDIR alone is long enough that t.TempDir()'s test-name segments push
// a socket beneath it over the edge. Same fallback the bus tests use.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atomicrepl")
	if err != nil {
		return t.TempDir()
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
