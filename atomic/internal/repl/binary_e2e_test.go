package repl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The one test in the package that runs the shipped command rather than the
// functions behind it. Everything else calls in-process, which structurally
// cannot see argv assembly, exit-code propagation through main, environment
// inheritance into a detached child, or whether interpreter state survives the
// process that set it — the feature's headline claim. Those are also the seams
// that have hidden real CLI bugs in this repo behind a green in-process suite.

const (
	// e2eBuildTimeout bounds `go build`. Generous because a cold module cache
	// links the whole binary; warm it is ~1s.
	e2eBuildTimeout = 5 * time.Minute
	// e2eRunTimeout bounds one atomic invocation. Every step is a local unix
	// round trip; anything near this bound is a wedge, not slowness.
	e2eRunTimeout = 60 * time.Second
	// e2eSessionName is short on purpose: it lands inside a socket path under the
	// sandbox HOME, and sun_path is 104 bytes on macOS.
	e2eSessionName = "e2e"
)

// e2eRepoConfig is the sandbox repo's .claude/atomic.toml. scope = "repo" makes
// scope resolution deterministic without a git repo: repoctx prefers the marker
// over `git rev-parse`. The idle window is a leak bound, not a behavior under
// test — a harness this test fails to stop retires itself a minute later.
const e2eRepoConfig = "scope = \"repo\"\n\n[repl]\nidle_timeout = \"60s\"\n"

// e2eInvalidTimeoutConfig is the same marker with an unusable idle_timeout,
// for the start-time warning.
const e2eInvalidTimeoutConfig = "scope = \"repo\"\n\n[repl]\nidle_timeout = \"not-a-duration\"\n"

// e2eEnv is one sandboxed invocation context: a built binary, a temp HOME holding
// every session file, and a temp repo that is the scope root.
type e2eEnv struct {
	bin  string
	home string
	repo string
	env  []string
}

// Drives start → eval → eval → stop as four separate OS processes, then reads
// the exit codes an agent routes on back out of the real binary.
func TestReplBinary_EndToEnd(t *testing.T) {
	requireE2ETools(t)

	env := newE2EEnv(t, buildAtomicBinary(t), e2eRepoConfig)

	// Registered before the spawn: `start` returns only once the harness is live,
	// so every line below can abort with a detached interpreter already running.
	// The guard reads meta from disk at cleanup time, so it is correct whether
	// the start succeeded, failed, or never happened.
	env.guardSession(t, e2eSessionName)

	// --- start ---------------------------------------------------------
	stdout, stderr, exit := env.run(t, "repl", "start", "--name", e2eSessionName, "--lang", "py", "--json")
	if exit != int(ExitOK) {
		t.Fatalf("start: exit = %d, want %d; stderr=%s", exit, ExitOK, stderr)
	}
	assertJSONObjectKeys(t, "start", stdout,
		"name", "root", "lang", "bin", "pid", "started_at", "alive", "already_running")
	var started startView
	decodeStrict(t, "start", stdout, &started)
	if started.Name != e2eSessionName || started.Lang != LangPython || !started.Alive || started.AlreadyRunning {
		t.Errorf("start view = %+v, unexpected", started)
	}
	if started.Root != env.repo {
		t.Errorf("start root = %q, want the scope root %q", started.Root, env.repo)
	}

	// The "--" is not needed here, but it is the documented way to pass a code
	// positional, so the e2e uses the documented form.
	stdout, stderr, exit = env.run(t, "repl", "eval", "--name", e2eSessionName, "--json", "--", "e2e_probe = 6 * 7")
	if exit != int(ExitOK) {
		t.Fatalf("eval (define): exit = %d, want %d; stderr=%s", exit, ExitOK, stderr)
	}
	assertJSONObjectKeys(t, "eval", stdout, "v", "ok", "stdout", "stderr", "value", "error", "truncated")
	var defined Response
	decodeStrict(t, "eval (define)", stdout, &defined)
	if !defined.OK || defined.Value != "" {
		t.Errorf("eval (define) = %+v, want ok with no value (a bare statement)", defined)
	}

	// The whole feature in one assertion: a separate process, sharing no memory
	// with the one above, sees the variable it set.
	stdout, stderr, exit = env.run(t, "repl", "eval", "--name", e2eSessionName, "--json", "e2e_probe + 1")
	if exit != int(ExitOK) {
		t.Fatalf("eval (read): exit = %d, want %d; stderr=%s", exit, ExitOK, stderr)
	}
	var read Response
	decodeStrict(t, "eval (read)", stdout, &read)
	if !read.OK || read.Value != "43" {
		t.Errorf("eval (read) = %+v, want value \"43\" — state did not survive the process boundary", read)
	}

	// --- list -----------------------------------------------------------
	stdout, stderr, exit = env.run(t, "repl", "list", "--json")
	if exit != int(ExitOK) {
		t.Fatalf("list: exit = %d, want %d; stderr=%s", exit, ExitOK, stderr)
	}
	assertJSONArrayKeys(t, "list", stdout, "name", "root", "lang", "bin", "pid", "started_at", "alive")
	var listed []sessionView
	decodeStrict(t, "list", stdout, &listed)
	if len(listed) != 1 {
		t.Fatalf("list returned %d sessions, want 1: %s", len(listed), stdout)
	}
	if listed[0].Name != e2eSessionName || listed[0].PID != started.PID || !listed[0].Alive {
		t.Errorf("listed session = %+v, want the running %q", listed[0], e2eSessionName)
	}

	// --- eval: the code raises (exit 3, distinct from a command failure) --
	stdout, _, exit = env.run(t, "repl", "eval", "--name", e2eSessionName, "--json", "--", "raise ValueError('e2e-boom')")
	if exit != int(ExitEvalException) {
		t.Fatalf("eval (raise): exit = %d, want %d", exit, ExitEvalException)
	}
	var raised Response
	decodeStrict(t, "eval (raise)", stdout, &raised)
	if raised.OK {
		t.Errorf("eval (raise) reported ok; response = %+v", raised)
	}
	for _, want := range []string{"ValueError: e2e-boom", "raise ValueError('e2e-boom')"} {
		if !strings.Contains(raised.Error, want) {
			t.Errorf("traceback does not contain %q; got:\n%s", want, raised.Error)
		}
	}

	// Still serving after the exception — an eval that threw is the code failing,
	// not the session.
	if _, _, exit = env.run(t, "repl", "eval", "--name", e2eSessionName, "1 + 1"); exit != int(ExitOK) {
		t.Errorf("eval after exception: exit = %d, want %d", exit, ExitOK)
	}

	// --- stop -----------------------------------------------------------
	if _, stderr, exit = env.run(t, "repl", "stop", "--name", e2eSessionName); exit != int(ExitOK) {
		t.Fatalf("stop: exit = %d, want %d; stderr=%s", exit, ExitOK, stderr)
	}

	// --- eval after stop: not found (exit 2) ----------------------------
	_, stderr, exit = env.run(t, "repl", "eval", "--name", e2eSessionName, "1 + 1")
	if exit != int(ExitNotFound) {
		t.Fatalf("eval after stop: exit = %d, want %d; stderr=%s", exit, ExitNotFound, stderr)
	}
	if !strings.Contains(stderr, "atomic repl start") {
		t.Errorf("not-found stderr = %q, want the start remedy", stderr)
	}
}

// The start-time config warning through the shipped binary. --bin is deliberately
// unresolvable: the diagnostic is about the config file, so it must reach stderr
// without a harness ever being spawned, and without this test leaking one.
func TestReplBinary_InvalidIdleTimeoutWarnsOnStart(t *testing.T) {
	requireE2ETools(t)

	env := newE2EEnv(t, buildAtomicBinary(t), e2eInvalidTimeoutConfig)

	_, stderr, exit := env.run(t, "repl", "start",
		"--name", "warnonly", "--lang", "py", "--bin", "/definitely/not/a/real/binary-xyz")

	if exit != int(ExitInterpreterUnavailable) {
		t.Fatalf("exit = %d, want %d; stderr=%s", exit, ExitInterpreterUnavailable, stderr)
	}
	for _, want := range []string{filepath.Join(env.repo, ".claude", "atomic.toml"), `"not-a-duration"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not name %s; got:\n%s", want, stderr)
		}
	}
}

// requireE2ETools skips when the Go toolchain or python3 is absent.
func requireE2ETools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not on PATH: %v", err)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 not on PATH: %v", err)
	}
}

// buildAtomicBinary builds cmd/atomic and returns the path to the binary.
func buildAtomicBinary(t *testing.T) string {
	t.Helper()

	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
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

// newE2EEnv builds a sandbox: a temp HOME, so every session file lands there
// rather than in the developer's real ~/.atomic, and a temp repo carrying
// repoConfig as its .claude/atomic.toml.
func newE2EEnv(t *testing.T, bin, repoConfig string) *e2eEnv {
	t.Helper()

	// shortTempDir roots under /tmp: the socket path is HOME-derived and sun_path
	// is 104 bytes.
	home := realPath(t, shortTempDir(t))
	repo := realPath(t, shortTempDir(t))

	configDir := filepath.Join(repo, ".claude")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "atomic.toml"), []byte(repoConfig), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	return &e2eEnv{
		bin:  bin,
		home: home,
		repo: repo,
		env: []string{
			// The sandbox itself: os.UserHomeDir reads HOME, so every path under
			// ~/.atomic/repl resolves inside the temp dir.
			"HOME=" + home,
			"PATH=" + os.Getenv("PATH"),
			// os.Getwd prefers PWD when it names the same directory as ".", keeping
			// the scope root spelled exactly as asserted.
			"PWD=" + repo,
			// This env is the child's whole environment, so harness.dir would land
			// on the built-in default anyway. Pinning it states the fixture's
			// dependency out loud: the config above is written to
			// ".claude/atomic.toml", and this is what says the binary looks there.
			"ATOMIC_HARNESS=.claude",
		},
	}
}

// run executes one atomic invocation in the sandbox and returns its streams and
// exit code. --no-update-check keeps the run off the network.
func (e *e2eEnv) run(t *testing.T, args ...string) (stdout, stderr string, exit int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), e2eRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.bin, append([]string{"--no-update-check"}, args...)...)
	cmd.Dir = e.repo
	cmd.Env = e.env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("atomic %s did not finish within %s", strings.Join(args, " "), e2eRunTimeout)
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		exit = exitErr.ExitCode()
	default:
		t.Fatalf("run atomic %s: %v", strings.Join(args, " "), err)
	}
	return outBuf.String(), errBuf.String(), exit
}

// guardSession is the backstop for the one thing this test can leak: a detached
// harness outliving a failing assertion path, since any t.Fatalf between start
// and stop skips the stop.
//
// It takes only the session name and resolves the pid from meta at cleanup time,
// never from a response body, so it can be registered before the spawn it guards
// and cannot be skipped by a failure while parsing that body. An absent meta
// means nothing to kill. defaultPidMatch is the package's own recycled-pid
// guard, so the kill can only land on the process this test started.
func (e *e2eEnv) guardSession(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		metaPath, err := MetaPath(e.home, e.repo, name)
		if err != nil {
			t.Errorf("leak guard: resolve meta path for %q: %v", name, err)
			return
		}
		meta, err := LoadMeta(metaPath)
		if err != nil {
			return
		}
		if meta.PID <= 0 || !defaultPidMatch(meta.PID, meta.StartedAt) {
			return
		}
		if err := syscall.Kill(meta.PID, syscall.SIGKILL); err != nil {
			t.Errorf("kill leaked harness pid %d: %v", meta.PID, err)
		}
	})
}

// realPath resolves symlinks so asserted sandbox paths match the spelling the
// binary resolves (/tmp is a symlink to /private/tmp on macOS).
func realPath(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	return resolved
}

// decodeStrict rejects unknown fields, so a field added to the wire shape without
// updating the documented one fails here rather than reaching an agent that
// strict-parses it.
func decodeStrict(t *testing.T, label, body string, v any) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("%s: decode into %T: %v; body=%s", label, v, err, body)
	}
}

// assertJSONObjectKeys pins the exact key set. Strict decoding catches an extra
// key; only this catches a missing one — and "every field is always present" is
// the contract precisely so a caller never tells absent from empty.
func assertJSONObjectKeys(t *testing.T, label, body string, want ...string) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatalf("%s: decode object: %v; body=%s", label, err, body)
	}
	assertKeySet(t, label, obj, want)
}

// assertJSONArrayKeys is assertJSONObjectKeys for a JSON array of objects.
func assertJSONArrayKeys(t *testing.T, label, body string, want ...string) {
	t.Helper()
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &arr); err != nil {
		t.Fatalf("%s: decode array: %v; body=%s", label, err, body)
	}
	for i, obj := range arr {
		assertKeySet(t, fmt.Sprintf("%s[%d]", label, i), obj, want)
	}
}

func assertKeySet(t *testing.T, label string, obj map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	if strings.Join(got, ",") != strings.Join(sorted, ",") {
		t.Errorf("%s: JSON keys = %v, want %v", label, got, sorted)
	}
}
