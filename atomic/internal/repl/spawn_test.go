package repl

import (
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

// stubSpawner stands in for a real interpreter: it records every call and, when
// asked to, brings the session's socket up in-process. That is what lets the
// concurrency test below assert "exactly one session" without a python3 or node
// on the machine and without a process to leak.
type stubSpawner struct {
	mu     sync.Mutex
	calls  []SpawnSpec
	listen bool // bring the socket up, as a real harness would
	err    error
}

func (s *stubSpawner) spawn(t *testing.T) SpawnFunc {
	t.Helper()
	return func(spec SpawnSpec) (int, error) {
		s.mu.Lock()
		s.calls = append(s.calls, spec)
		s.mu.Unlock()

		if s.err != nil {
			return 0, s.err
		}
		if s.listen {
			serveStubSocket(t, spec.SocketPath)
		}
		return os.Getpid(), nil
	}
}

func (s *stubSpawner) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubSpawner) lastSpec(t *testing.T) SpawnSpec {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		t.Fatal("spawn was never called")
	}
	return s.calls[len(s.calls)-1]
}

// serveStubSocket listens at path and accepts-and-drops, so IsLive sees a live
// session. The listener is closed on cleanup whether or not the test got there.
func serveStubSocket(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("stub listen %s: %v", path, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln
}

// stubStartOptions is a start that never touches a real interpreter: the bin is
// one every POSIX machine has, and the spawner is injected.
func stubStartOptions(t *testing.T, home string, spawner *stubSpawner) StartOptions {
	t.Helper()
	return StartOptions{
		Home:         home,
		ScopeRoot:    "/repo",
		Name:         "work",
		Lang:         LangPython,
		Bin:          "sh", // resolvable, never executed — the spawner is a stub
		IdleTimeout:  time.Hour,
		Spawn:        spawner.spawn(t),
		WaitTimeout:  5 * time.Second,
		PollInterval: 5 * time.Millisecond,
	}
}

func TestEnsureStarted_ConcurrentStartsProduceExactlyOneSession(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)

	const racers = 8
	var wg sync.WaitGroup
	metas := make([]Meta, racers)
	running := make([]bool, racers)
	errs := make([]error, racers)

	ready := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready // release them together, so they really do race
			metas[i], running[i], errs[i] = EnsureStarted(opts)
		}()
	}
	close(ready)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: EnsureStarted: %v", i, err)
		}
	}
	// The whole point of the flock: probe-and-spawn is one decision, so a
	// loser blocks, wakes to a live session, and never spawns a second
	// harness over the first one's socket.
	if got := spawner.count(); got != 1 {
		t.Errorf("spawned %d harnesses, want exactly 1", got)
	}
	spawned := 0
	for _, alreadyRunning := range running {
		if !alreadyRunning {
			spawned++
		}
	}
	if spawned != 1 {
		t.Errorf("%d racers report having started the session, want exactly 1", spawned)
	}

	sock, err := SocketPath(home, opts.ScopeRoot, opts.Name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if !IsLive(sock) {
		t.Error("no live session after the race")
	}
	for i, meta := range metas {
		if meta.Socket != sock {
			t.Errorf("racer %d got socket %q, want %q — every racer must describe the one session", i, meta.Socket, sock)
		}
	}
}

func TestEnsureStarted_ALiveSessionIsANoOp(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)

	first, alreadyRunning, err := EnsureStarted(opts)
	if err != nil {
		t.Fatalf("first EnsureStarted: %v", err)
	}
	if alreadyRunning {
		t.Fatal("the first start reports already-running")
	}

	second, alreadyRunning, err := EnsureStarted(opts)
	if err != nil {
		t.Fatalf("second EnsureStarted: %v", err)
	}
	if !alreadyRunning {
		t.Error("a second start on a live session reports having spawned; it must report already-running instead")
	}
	if got := spawner.count(); got != 1 {
		t.Errorf("spawned %d times, want 1 — a duplicate harness would silently orphan the first one's state", got)
	}
	// Equal, not ==: the second answer comes back through the meta file, so it
	// carries no monotonic reading.
	if second.PID != first.PID || !second.StartedAt.Equal(first.StartedAt) {
		t.Errorf("already-running returned %+v, want the live session's own meta %+v", second, first)
	}
}

func TestEnsureStarted_RemovesAStaleSocketBeforeSpawning(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)

	sock, err := SocketPath(home, opts.ScopeRoot, opts.Name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// What a crashed harness (or a host reboot) leaves behind. Nothing is
	// listening, and bind refuses a path that already exists.
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}

	if _, alreadyRunning, err := EnsureStarted(opts); err != nil {
		t.Fatalf("EnsureStarted over a stale socket: %v", err)
	} else if alreadyRunning {
		t.Error("a stale socket file was mistaken for a live session")
	}
	if !IsLive(sock) {
		t.Error("session is not live after recovering from a stale socket")
	}
}

func TestEnsureStarted_CreatesTheSessionDirAndMetaWithTightModes(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)

	if _, _, err := EnsureStarted(opts); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}

	// A session is code execution into a process that may hold --env secrets,
	// so neither the directory nor the meta file is left at the house default.
	dir, err := os.Stat(SessionDir(home, opts.ScopeRoot))
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("session dir mode = %o, want 700", perm)
	}

	metaPath, err := MetaPath(home, opts.ScopeRoot, opts.Name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	meta, err := os.Stat(metaPath)
	if err != nil {
		t.Fatalf("stat meta: %v", err)
	}
	if perm := meta.Mode().Perm(); perm != 0o600 {
		t.Errorf("meta mode = %o, want 600", perm)
	}
}

func TestEnsureStarted_RecordsWhatALaterProcessNeeds(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)
	before := time.Now()

	meta, _, err := EnsureStarted(opts)
	if err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}

	if meta.Name != opts.Name || meta.Lang != opts.Lang || meta.Root != opts.ScopeRoot {
		t.Errorf("meta = %+v, want name/lang/root from the start options", meta)
	}
	if meta.PID == 0 {
		t.Error("meta records no pid; the timeout escalation has nothing to signal")
	}
	if !strings.HasSuffix(meta.Bin, "sh") {
		t.Errorf("meta bin = %q, want the resolved interpreter path", meta.Bin)
	}
	if meta.StartedAt.Before(before) {
		t.Errorf("meta started_at %v predates the start; the recycled-pid guard compares against it", meta.StartedAt)
	}

	// Written to disk, not just returned: the next `eval` is a different
	// process with none of this in memory.
	metaPath, err := MetaPath(home, opts.ScopeRoot, opts.Name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	stored, err := LoadMeta(metaPath)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if stored.PID != meta.PID {
		t.Errorf("stored pid = %d, want %d", stored.PID, meta.PID)
	}
}

func TestEnsureStarted_PassesTheHarnessWhatItNeeds(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)
	opts.IdleTimeout = 90 * time.Second
	opts.Env = []string{"REPL_PROBE=probe-value"}

	if _, _, err := EnsureStarted(opts); err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}

	spec := spawner.lastSpec(t)
	if spec.IdleTimeout != opts.IdleTimeout {
		t.Errorf("spec idle timeout = %v, want %v", spec.IdleTimeout, opts.IdleTimeout)
	}
	// The harness's cwd is the scope root, so relative paths in eval'd code
	// resolve against the repo rather than wherever `start` was typed.
	if spec.ScopeRoot != opts.ScopeRoot {
		t.Errorf("spec scope root = %q, want %q", spec.ScopeRoot, opts.ScopeRoot)
	}
	if !containsEntry(spec.Env, "REPL_PROBE=probe-value") {
		t.Error("--env entries did not reach the spawn environment")
	}
	if !containsPrefix(spec.Env, "PATH=") {
		t.Error("the spawn environment dropped the parent's own; the interpreter needs PATH")
	}

	// The script is materialized under the name the module system needs — a
	// Node harness written as .js would inherit an unrelated ancestor
	// package.json's "type".
	wantName, err := HarnessFilename(opts.Lang)
	if err != nil {
		t.Fatalf("HarnessFilename: %v", err)
	}
	if got := filepath.Base(spec.ScriptPath); got != wantName {
		t.Errorf("script materialized as %q, want %q", got, wantName)
	}
	if _, err := os.Stat(spec.ScriptPath); err != nil {
		t.Errorf("harness script is not on disk at spawn time: %v", err)
	}
}

func TestEnsureStarted_AnUnresolvableInterpreterFailsBeforeAnySpawn(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)
	opts.Bin = filepath.Join(t.TempDir(), "definitely-not-an-interpreter")

	_, _, err := EnsureStarted(opts)
	if !errors.Is(err, ErrInterpreterUnavailable) {
		t.Fatalf("EnsureStarted with an unresolvable --bin = %v, want ErrInterpreterUnavailable", err)
	}
	if !strings.Contains(err.Error(), opts.Bin) {
		t.Errorf("error %q does not name the binary that could not be resolved", err)
	}
	if got := spawner.count(); got != 0 {
		t.Errorf("spawned %d times after an unresolvable interpreter; the check must precede any spawn", got)
	}
}

func TestEnsureStarted_RejectsANameThatIsNotAPathComponent(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	opts := stubStartOptions(t, home, spawner)
	opts.Name = "../escape"

	if _, _, err := EnsureStarted(opts); err == nil {
		t.Fatal("EnsureStarted accepted a traversing session name")
	}
	if got := spawner.count(); got != 0 {
		t.Errorf("spawned %d times for an invalid name; nothing is touched before validation", got)
	}
}

func TestEnsureStarted_ReportsASpawnFailureWithoutWritingMeta(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{err: errors.New("exec format error")}
	opts := stubStartOptions(t, home, spawner)

	_, _, err := EnsureStarted(opts)
	if err == nil {
		t.Fatal("EnsureStarted reported success after the spawn failed")
	}
	if !strings.Contains(err.Error(), "exec format error") {
		t.Errorf("error %q drops the spawn failure", err)
	}
	assertNoMeta(t, home, opts)
}

func TestEnsureStarted_ReportsAHarnessThatNeverBinds(t *testing.T) {
	home := shortTempDir(t)
	// A spawn that "succeeds" but never listens — the shape of an interpreter
	// that starts and dies before binding.
	spawner := &stubSpawner{listen: false}
	opts := stubStartOptions(t, home, spawner)
	opts.WaitTimeout = 150 * time.Millisecond

	_, _, err := EnsureStarted(opts)
	if err == nil {
		t.Fatal("EnsureStarted reported success for a harness that never bound its socket")
	}
	if !strings.Contains(err.Error(), "did not accept") {
		t.Errorf("error %q does not say the socket never came up", err)
	}
	// No meta means later verbs report not-found, which is true — rather than
	// pointing at a pid that is not serving anything.
	assertNoMeta(t, home, opts)
}

func assertNoMeta(t *testing.T, home string, opts StartOptions) {
	t.Helper()
	metaPath, err := MetaPath(home, opts.ScopeRoot, opts.Name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	if _, err := os.Stat(metaPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("meta was written despite the start failing (stat err %v)", err)
	}
}

func TestMaterializeHarness_AlwaysRewritesFromTheEmbeddedBytes(t *testing.T) {
	for _, lang := range []string{LangPython, LangNode} {
		t.Run(lang, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "session")
			want, err := HarnessScript(lang)
			if err != nil {
				t.Fatalf("HarnessScript: %v", err)
			}

			path, err := materializeHarness(dir, lang)
			if err != nil {
				t.Fatalf("materializeHarness: %v", err)
			}
			wantName, err := HarnessFilename(lang)
			if err != nil {
				t.Fatalf("HarnessFilename: %v", err)
			}
			if got := filepath.Base(path); got != wantName {
				t.Errorf("materialized as %q, want %q", got, wantName)
			}

			// A stale or tampered copy is never trusted: every start rewrites
			// the script from the bytes compiled into this binary.
			if err := os.WriteFile(path, []byte("print('tampered')\n"), 0o700); err != nil {
				t.Fatalf("tamper: %v", err)
			}
			if _, err := materializeHarness(dir, lang); err != nil {
				t.Fatalf("second materializeHarness: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != string(want) {
				t.Error("materializeHarness left a tampered script in place")
			}

			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("stat dir: %v", err)
			}
			if perm := info.Mode().Perm(); perm != 0o700 {
				t.Errorf("materialization dir mode = %o, want 700", perm)
			}
		})
	}
}

func TestIsLive(t *testing.T) {
	dir := shortTempDir(t)

	absent := filepath.Join(dir, "absent.sock")
	if IsLive(absent) {
		t.Error("IsLive reports a session at a path with no socket")
	}

	// A stale socket file with nothing behind it: file existence is not
	// liveness, which is why every probe dials.
	stale := filepath.Join(dir, "stale.sock")
	if err := os.WriteFile(stale, nil, 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}
	if IsLive(stale) {
		t.Error("IsLive trusts a stale socket file")
	}

	live := filepath.Join(dir, "live.sock")
	serveStubSocket(t, live)
	if !IsLive(live) {
		t.Error("IsLive reports a listening socket as dead")
	}
}

func TestResolveInterpreter(t *testing.T) {
	t.Run("an override wins over the language default", func(t *testing.T) {
		got, err := ResolveInterpreter(LangPython, "sh")
		if err != nil {
			t.Fatalf("ResolveInterpreter: %v", err)
		}
		if !strings.HasSuffix(got, "sh") {
			t.Errorf("resolved %q, want the sh on PATH", got)
		}
	})

	t.Run("an unresolvable override is not a usage error", func(t *testing.T) {
		_, err := ResolveInterpreter(LangPython, "/nonexistent/interpreter")
		// Distinct from a usage mistake so an agent can tell "install it or
		// point --bin somewhere real" apart from "I wrote the command wrong".
		if !errors.Is(err, ErrInterpreterUnavailable) {
			t.Fatalf("err = %v, want ErrInterpreterUnavailable", err)
		}
	})

	t.Run("an unknown language is rejected", func(t *testing.T) {
		if _, err := ResolveInterpreter("ruby", ""); err == nil {
			t.Fatal("ResolveInterpreter accepted an unknown language")
		}
	})
}

// TestDefaultSpawn_BringsUpARealHarness is the one test here that runs a real
// interpreter through the real spawn path. The stubbed tests above prove the
// decision logic; only this one proves the argv, the detachment, the working
// directory, and the environment are what a harness can actually start from.
func TestDefaultSpawn_BringsUpARealHarness(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	opts := StartOptions{
		Home:      home,
		ScopeRoot: scopeRoot,
		Name:      "e2e",
		Lang:      LangPython,
		// 90s, not a round minute, on purpose: the harnesses parse this flag
		// as a float, so a Go duration string ("1m30s") would fail to start.
		IdleTimeout:  90 * time.Second,
		Env:          []string{"REPL_PROBE=probe-value"},
		WaitTimeout:  20 * time.Second,
		PollInterval: 25 * time.Millisecond,
	}

	meta, alreadyRunning, err := EnsureStarted(opts)
	if err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	stopHarnessOnCleanup(t, meta)
	if alreadyRunning {
		t.Fatal("a fresh session reports already-running")
	}

	// A real socket, bound by the harness, at the mode a socket carrying
	// --env secrets has to have.
	info, err := os.Stat(meta.Socket)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %o, want 600", perm)
	}

	// The harness's cwd is the scope root, so relative paths in eval'd code
	// resolve against the repo. EvalSymlinks because getcwd reports the real
	// path and /tmp is a symlink on macOS.
	wantCWD, err := filepath.EvalSymlinks(scopeRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got := evalOnce(t, meta, "import os\nos.getcwd()"); got != quotePy(wantCWD) {
		t.Errorf("harness cwd = %s, want %s", got, quotePy(wantCWD))
	}

	// --env reached the process, and the parent's own environment came along.
	if got := evalOnce(t, meta, "import os\nos.environ['REPL_PROBE']"); got != quotePy("probe-value") {
		t.Errorf("REPL_PROBE = %s, want %s", got, quotePy("probe-value"))
	}

	// State survives between connections — which is the whole feature, and
	// which no stub can demonstrate.
	if got := evalOnce(t, meta, "carried = 41"); got != "" {
		t.Errorf("value = %q for a statement, want empty", got)
	}
	if got := evalOnce(t, meta, "carried + 1"); got != "42" {
		t.Errorf("value = %q on a second connection, want %q — state did not persist", got, "42")
	}
}

func evalOnce(t *testing.T, meta Meta, code string) string {
	t.Helper()
	resp, err := Eval(Session{SocketPath: meta.Socket, Meta: meta}, code, EvalOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("Eval(%q): %v", code, err)
	}
	if !resp.OK {
		t.Fatalf("Eval(%q) failed: %s", code, resp.Error)
	}
	return resp.Value
}

func quotePy(s string) string { return "'" + s + "'" }

// stopHarnessOnCleanup ends a real harness: shutdown first, then SIGKILL if it
// is still there — and only ever at a pid whose identity just verified, never
// at a bare number read off disk.
func stopHarnessOnCleanup(t *testing.T, meta Meta) {
	t.Helper()
	t.Cleanup(func() {
		if client, err := Dial(meta.Socket, 2*time.Second); err == nil {
			_, _ = client.Do(Request{V: ProtocolVersion, Op: OpShutdown})
			client.Close()
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if !defaultPidMatch(meta.PID, meta.StartedAt) {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		if defaultPidMatch(meta.PID, meta.StartedAt) {
			_ = syscall.Kill(meta.PID, syscall.SIGKILL)
			t.Errorf("harness pid %d ignored shutdown and had to be killed", meta.PID)
		}
	})
}

func containsEntry(entries []string, want string) bool {
	for _, entry := range entries {
		if entry == want {
			return true
		}
	}
	return false
}

func containsPrefix(entries []string, prefix string) bool {
	for _, entry := range entries {
		if strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
}
