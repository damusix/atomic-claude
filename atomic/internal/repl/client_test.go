package repl

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// stubHarness is an in-process stand-in for a session, so the client's timeout
// and escalation paths are exercised without a real interpreter — including the
// case a real one cannot be made to reproduce on demand: a harness that ignores
// SIGINT.
type stubHarness struct {
	socketPath string
	metaPath   string
	ln         net.Listener
}

// startStubHarness serves socketPath, answering each request through handler.
// A handler that returns reply=false reads the request and then says nothing,
// which is the shape of a harness wedged inside an eval.
func startStubHarness(t *testing.T, handler func(Request) (Response, bool)) *stubHarness {
	t.Helper()

	dir := shortTempDir(t)
	h := &stubHarness{
		socketPath: filepath.Join(dir, "work.sock"),
		metaPath:   filepath.Join(dir, "work.meta.json"),
	}

	ln, err := net.Listen("unix", h.socketPath)
	if err != nil {
		t.Fatalf("stub listen: %v", err)
	}
	h.ln = ln
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req Request
				if err := json.Unmarshal(line, &req); err != nil {
					return
				}
				resp, reply := handler(req)
				if !reply {
					// Hold the connection open with nothing coming, which is
					// what an eval stuck in a hot loop looks like from here.
					<-make(chan struct{})
				}
				frame, err := json.Marshal(resp)
				if err != nil {
					return
				}
				conn.Write(append(frame, '\n'))
			}()
		}
	}()
	return h
}

func (h *stubHarness) session(t *testing.T, meta Meta) Session {
	t.Helper()
	meta.Socket = h.socketPath
	if err := meta.Save(h.metaPath); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	return Session{SocketPath: h.socketPath, MetaPath: h.metaPath, Meta: meta}
}

func echoHandler(resp Response) func(Request) (Response, bool) {
	return func(Request) (Response, bool) { return resp, true }
}

func okResponse(value string) Response {
	return Response{V: ProtocolVersion, OK: true, Value: value}
}

// signalRecorder captures the escalation's signals instead of delivering them,
// so a test never sends a real signal at a real pid.
type signalRecorder struct {
	mu   sync.Mutex
	sent []os.Signal
}

func (r *signalRecorder) fn() SignalFunc {
	return func(_ int, sig os.Signal) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.sent = append(r.sent, sig)
		return nil
	}
}

func (r *signalRecorder) signals() []os.Signal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]os.Signal(nil), r.sent...)
}

// fastEscalation keeps every wait off the wall clock: the grace is a few
// milliseconds, not the seconds a person would wait.
func fastEscalation(opts EvalOptions) EvalOptions {
	opts.Timeout = 150 * time.Millisecond
	opts.Grace = 20 * time.Millisecond
	opts.GracePoll = time.Millisecond
	return opts
}

func TestClient_RoundTrip(t *testing.T) {
	h := startStubHarness(t, echoHandler(okResponse("42")))

	client, err := Dial(h.socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	resp, err := client.Do(Request{V: ProtocolVersion, Op: OpPing})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !resp.OK || resp.Value != "42" {
		t.Errorf("response = %+v, want the harness's own", resp)
	}
}

func TestClient_RefusesAProtocolVersionItDoesNotSpeak(t *testing.T) {
	// A harness spawned by an older binary outliving an `atomic update`.
	h := startStubHarness(t, echoHandler(Response{V: ProtocolVersion + 7, OK: true}))

	client, err := Dial(h.socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.Do(Request{V: ProtocolVersion, Op: OpPing})
	if err == nil {
		t.Fatal("Do accepted a response from a protocol version it does not speak")
	}
	var mismatch *ProtocolMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want a *ProtocolMismatchError so callers can route on it", err)
	}
	// Naming the fix matters more than naming the numbers: the session has to
	// be replaced, and nothing else will do it.
	for _, want := range []string{"atomic repl stop", "atomic repl start"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q as the fix", err, want)
		}
	}
}

func TestDial_ClassifiesAnAbsentSocketApartFromADeadOne(t *testing.T) {
	dir := shortTempDir(t)

	// Never started (or reaped): the two are deliberately indistinguishable.
	_, err := Dial(filepath.Join(dir, "absent.sock"), time.Second)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("dialing an absent socket = %v, want ErrSessionNotFound", err)
	}

	// A socket file with nothing behind it: the harness crashed or the host
	// rebooted. That is a dead session, not an absent one — reporting it as
	// absent would hide the state loss behind a "just run start" remedy.
	stale := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}
	_, err = Dial(stale, time.Second)
	if !errors.Is(err, ErrSessionDead) {
		t.Errorf("dialing a stale socket = %v, want ErrSessionDead", err)
	}
}

func TestEval_ReturnsTheHarnessResponse(t *testing.T) {
	h := startStubHarness(t, func(req Request) (Response, bool) {
		if req.Op != OpEval {
			t.Errorf("op = %q, want %q", req.Op, OpEval)
		}
		return okResponse(req.Code), true
	})
	sess := h.session(t, Meta{Name: "work", PID: os.Getpid(), StartedAt: time.Now()})

	resp, err := Eval(sess, "6 * 7", fastEscalation(EvalOptions{}))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if resp.Value != "6 * 7" {
		t.Errorf("value = %q, want the code echoed back", resp.Value)
	}
	assertSessionFilesIntact(t, sess)
}

func TestEval_AnEvalExceptionIsAResponseNotATransportError(t *testing.T) {
	// The command worked; the code did not. The distinction is the whole
	// reason exit 3 exists, so it cannot be collapsed into an error here.
	h := startStubHarness(t, echoHandler(Response{V: ProtocolVersion, OK: false, Error: "ValueError: boom"}))
	sess := h.session(t, Meta{Name: "work", PID: os.Getpid(), StartedAt: time.Now()})

	resp, err := Eval(sess, "raise ValueError('boom')", fastEscalation(EvalOptions{}))
	if err != nil {
		t.Fatalf("Eval on a raising eval = %v, want the failure carried in the response", err)
	}
	if resp.OK || resp.Error == "" {
		t.Errorf("response = %+v, want ok=false carrying the traceback", resp)
	}
	assertSessionFilesIntact(t, sess)
}

func TestEval_TimeoutEscalatesSigintThenSigkillAndRemovesTheSession(t *testing.T) {
	// A harness that never answers and never dies — the Node/Python asymmetry
	// made concrete: SIGINT kills a Node harness outright, while a Python one
	// catches KeyboardInterrupt and keeps serving. The escalation must end the
	// session either way, so it is written against the harness that survives.
	h := startStubHarness(t, func(Request) (Response, bool) { return Response{}, false })
	sess := h.session(t, Meta{Name: "work", PID: 4242, StartedAt: time.Now()})

	signals := &signalRecorder{}
	_, err := Eval(sess, "while True: pass", fastEscalation(EvalOptions{
		Signal:   signals.fn(),
		PidMatch: func(int, time.Time) bool { return true }, // never dies
	}))

	if !errors.Is(err, ErrEvalTimeout) {
		t.Fatalf("Eval = %v, want ErrEvalTimeout", err)
	}
	want := []os.Signal{syscall.SIGINT, syscall.SIGKILL}
	got := signals.signals()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("signals = %v, want %v — SIGINT first, SIGKILL only after the grace", got, want)
	}
	// The session is gone: a later call gets the same not-found answer a
	// never-started name gets, rather than dialing a socket nothing serves.
	assertSessionFilesRemoved(t, sess)
}

func TestEval_TimeoutStopsAtSigintWhenTheHarnessDies(t *testing.T) {
	h := startStubHarness(t, func(Request) (Response, bool) { return Response{}, false })
	sess := h.session(t, Meta{Name: "work", PID: 4242, StartedAt: time.Now()})

	signals := &signalRecorder{}
	var mu sync.Mutex
	interrupted := false
	_, err := Eval(sess, "while True: pass", fastEscalation(EvalOptions{
		Signal: func(pid int, sig os.Signal) error {
			mu.Lock()
			if sig == syscall.SIGINT {
				interrupted = true
			}
			mu.Unlock()
			return signals.fn()(pid, sig)
		},
		// The process is gone once the interrupt lands, which is what a Node
		// harness does: it installs no SIGINT handler, so the signal's default
		// disposition terminates it mid-eval.
		PidMatch: func(int, time.Time) bool {
			mu.Lock()
			defer mu.Unlock()
			return !interrupted
		},
	}))

	if !errors.Is(err, ErrEvalTimeout) {
		t.Fatalf("Eval = %v, want ErrEvalTimeout", err)
	}
	if got := signals.signals(); len(got) != 1 || got[0] != syscall.SIGINT {
		t.Errorf("signals = %v, want only SIGINT — nothing escalates at a process that already exited", got)
	}
	assertSessionFilesRemoved(t, sess)
}

func TestEval_TimeoutNeverSignalsARecycledPid(t *testing.T) {
	h := startStubHarness(t, func(Request) (Response, bool) { return Response{}, false })
	sess := h.session(t, Meta{Name: "work", PID: 4242, StartedAt: time.Now().Add(-time.Hour)})

	signals := &signalRecorder{}
	_, err := Eval(sess, "while True: pass", fastEscalation(EvalOptions{
		Signal: signals.fn(),
		// The pid is live but is not this session's any more — by the time an
		// eval times out, the number may name anything on the machine.
		PidMatch: func(int, time.Time) bool { return false },
	}))

	if !errors.Is(err, ErrEvalTimeout) {
		t.Fatalf("Eval = %v, want ErrEvalTimeout", err)
	}
	if got := signals.signals(); len(got) != 0 {
		t.Errorf("signals = %v, want none — a pid whose identity did not verify is never signaled", got)
	}
	assertSessionFilesRemoved(t, sess)
}

func TestEval_TimeoutNeverSignalsAnUnrecordedPid(t *testing.T) {
	h := startStubHarness(t, func(Request) (Response, bool) { return Response{}, false })
	// Meta with no pid: a session whose meta was written by something older,
	// or truncated. Signaling pid 0 is a signal to the whole process group.
	sess := h.session(t, Meta{Name: "work", StartedAt: time.Now()})

	signals := &signalRecorder{}
	_, err := Eval(sess, "while True: pass", fastEscalation(EvalOptions{
		Signal:   signals.fn(),
		PidMatch: func(int, time.Time) bool { return true },
	}))

	if !errors.Is(err, ErrEvalTimeout) {
		t.Fatalf("Eval = %v, want ErrEvalTimeout", err)
	}
	if got := signals.signals(); len(got) != 0 {
		t.Errorf("signals = %v, want none for an unrecorded pid", got)
	}
	assertSessionFilesRemoved(t, sess)
}

func TestDefaultPidMatch_AgainstARealProcess(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep not on PATH: %v", err)
	}

	startedAt := time.Now()
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("sleep %d did not exit after being killed", cmd.Process.Pid)
		}
	})

	pid := cmd.Process.Pid
	if !defaultPidMatch(pid, startedAt) {
		t.Errorf("defaultPidMatch(%d, %v) = false for a process just started", pid, startedAt)
	}
	// The recycled-pid case: the same live pid, a session that started long
	// before it did.
	if defaultPidMatch(pid, startedAt.Add(-24*time.Hour)) {
		t.Errorf("defaultPidMatch(%d, a day ago) = true; a recycled pid would be signaled", pid)
	}

	_ = cmd.Process.Kill()
	<-done
	if defaultPidMatch(pid, startedAt) {
		t.Errorf("defaultPidMatch(%d) = true after the process exited", pid)
	}
}

func TestDefaultPidMatch_AZombieIsNotAMatch(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("sleep not on PATH: %v", err)
	}

	startedAt := time.Now()
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := cmd.Process.Pid
	// Release, then kill, then never wait: an exited-but-unreaped child, which
	// is exactly the shape ps still reports elapsed time for.
	_ = cmd.Process.Release()
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill: %v", err)
	}
	t.Cleanup(func() { _, _ = syscall.Wait4(pid, nil, syscall.WNOHANG, nil) })

	// The corpse appears within milliseconds; the bound is only so a failure
	// reports rather than hangs.
	deadline := time.Now().Add(5 * time.Second)
	for defaultPidMatch(pid, startedAt) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if defaultPidMatch(pid, startedAt) {
		t.Error("defaultPidMatch = true for a killed, unreaped process; a corpse is not something to signal or wait on")
	}
}

func TestParseETime(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{in: "05", want: 5 * time.Second},
		{in: "00:07", want: 7 * time.Second},
		{in: "  01:30  ", want: 90 * time.Second},
		{in: "02:03:04", want: 2*time.Hour + 3*time.Minute + 4*time.Second},
		{in: "1-02:03:04", want: 26*time.Hour + 3*time.Minute + 4*time.Second},
		{in: "", bad: true},
		{in: "not-a-time", bad: true},
		{in: "1:2:3:4", bad: true},
	}
	for _, tc := range tests {
		got, err := parseETime(tc.in)
		if tc.bad {
			if err == nil {
				t.Errorf("parseETime(%q) = %v, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseETime(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseETime(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func assertSessionFilesRemoved(t *testing.T, sess Session) {
	t.Helper()
	for _, path := range []string{sess.SocketPath, sess.MetaPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survives the timeout escalation (stat err %v); the session must be gone", path, err)
		}
	}
}

func assertSessionFilesIntact(t *testing.T, sess Session) {
	t.Helper()
	for _, path := range []string{sess.SocketPath, sess.MetaPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed by a successful eval: %v", path, err)
		}
	}
}
