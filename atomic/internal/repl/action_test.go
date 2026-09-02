package repl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// startStubSessionAt seeds a session at exactly the socket/meta paths action.go's
// own resolution computes, so the verbs are exercised through production's path
// derivation without a real interpreter. Mirrors client_test.go's
// startStubHarness, but at a caller-chosen deterministic path.
func startStubSessionAt(t *testing.T, home, scopeRoot, name string, meta Meta, handler func(Request) (Response, bool)) Session {
	t.Helper()

	sockPath, err := SocketPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	metaPath, err := MetaPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
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
					// Holds the connection open with nothing coming: the shape of a
					// harness wedged inside an eval.
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

	meta.Name = name
	meta.Socket = sockPath
	meta.Root = scopeRoot
	if err := meta.Save(metaPath); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	return Session{SocketPath: sockPath, MetaPath: metaPath, Meta: meta}
}

// seedDeadSession writes a meta file plus a plain non-listening file at the
// socket path — file existence with nothing bound behind it, exactly what a
// harness that crashed without cleaning up leaves on disk.
func seedDeadSession(t *testing.T, home, scopeRoot, name string, meta Meta) Session {
	t.Helper()

	sockPath, err := SocketPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	metaPath, err := MetaPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}

	meta.Name = name
	meta.Socket = sockPath
	meta.Root = scopeRoot
	if err := meta.Save(metaPath); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	return Session{SocketPath: sockPath, MetaPath: metaPath, Meta: meta}
}

// seedSocketlessSession writes a meta file and no socket at all — the window a
// `stop` opens between acking the shutdown and the harness finishing the removal
// of its own files. findSession succeeds here; the dial that follows does not.
func seedSocketlessSession(t *testing.T, home, scopeRoot, name string, meta Meta) Session {
	t.Helper()

	sockPath, err := SocketPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	metaPath, err := MetaPath(home, scopeRoot, name)
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}

	meta.Name = name
	meta.Socket = sockPath
	meta.Root = scopeRoot
	if err := meta.Save(metaPath); err != nil {
		t.Fatalf("save meta: %v", err)
	}
	return Session{SocketPath: sockPath, MetaPath: metaPath, Meta: meta}
}

// captureStderr redirects os.Stderr for fn and returns what was written. Every
// action here writes errors straight to os.Stderr, so asserting on that needs a
// real fd swap, not an injected io.Writer.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr pipe writer: %v", err)
	}
	os.Stderr = orig
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return string(b)
}

// --- dispatch ---------------------------------------------------------

func TestReplAction_EmptyArgsIsUsageError(t *testing.T) {
	home := shortTempDir(t)
	cwd := shortTempDir(t)
	var out bytes.Buffer
	if code := ReplAction(nil, home, cwd, "", nil, &out); code != int(ExitUsage) {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestReplAction_RoutesEachVerbAndRejectsUnknownOnes(t *testing.T) {
	home := shortTempDir(t)
	cwd := shortTempDir(t)
	var out bytes.Buffer
	var code int

	stderr := captureStderr(t, func() {
		code = ReplAction([]string{"frobnicate"}, home, cwd, "", nil, &out)
	})
	if code != int(ExitUsage) || !strings.Contains(stderr, "unknown verb") {
		t.Errorf("unknown verb: exit=%d stderr=%q", code, stderr)
	}

	for _, verb := range []string{"start", "eval", "list", "status", "reset", "stop"} {
		verb := verb
		stderr := captureStderr(t, func() {
			code = ReplAction([]string{verb}, home, cwd, "", nil, &out)
		})
		if strings.Contains(stderr, "unknown verb") {
			t.Errorf("verb %q was rejected as unknown: stderr=%q", verb, stderr)
		}
	}
}

// --- start --------------------------------------------------------------

func TestStartAction_MissingRequiredFlagsIsUsageError(t *testing.T) {
	home := shortTempDir(t)
	var out bytes.Buffer
	if code := startAction([]string{"--lang", "py"}, home, []string{"/repo"}, nil, &out); code != int(ExitUsage) {
		t.Errorf("missing --name: exit = %d, want %d", code, ExitUsage)
	}
	if code := startAction([]string{"--name", "s"}, home, []string{"/repo"}, nil, &out); code != int(ExitUsage) {
		t.Errorf("missing --lang: exit = %d, want %d", code, ExitUsage)
	}
}

func TestStartAction_UnresolvableBinExitsInterpreterUnavailable(t *testing.T) {
	home := shortTempDir(t)
	var out bytes.Buffer
	code := startAction(
		[]string{"--name", "s", "--lang", "py", "--bin", "/definitely/not/a/real/binary-xyz"},
		home, []string{"/repo"}, nil, &out,
	)
	if code != int(ExitInterpreterUnavailable) {
		t.Errorf("exit = %d, want %d", code, ExitInterpreterUnavailable)
	}
}

func TestStartAction_SpawnsThenReportsAlreadyRunningWithoutRespawning(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	roots := []string{"/repo"}
	args := []string{"--name", "s", "--lang", "py", "--bin", "sh"}

	var out bytes.Buffer
	if code := startAction(args, home, roots, spawner.spawn(t), &out); code != int(ExitOK) {
		t.Fatalf("first start: exit = %d", code)
	}
	if spawner.count() != 1 {
		t.Fatalf("spawn count after first start = %d, want 1", spawner.count())
	}

	out.Reset()
	if code := startAction(args, home, roots, spawner.spawn(t), &out); code != int(ExitOK) {
		t.Fatalf("second start: exit = %d", code)
	}
	if spawner.count() != 1 {
		t.Errorf("spawn count after second start = %d, want still 1 (already running)", spawner.count())
	}
	if !strings.Contains(out.String(), "already running") {
		t.Errorf("second start output = %q, want it to mention already running", out.String())
	}
}

func TestStartAction_JSONOutput(t *testing.T) {
	home := shortTempDir(t)
	spawner := &stubSpawner{listen: true}
	var out bytes.Buffer
	code := startAction(
		[]string{"--name", "s", "--lang", "py", "--bin", "sh", "--json"},
		home, []string{"/repo"}, spawner.spawn(t), &out,
	)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var view startView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("decode JSON: %v; body=%s", err, out.String())
	}
	if view.Name != "s" || view.Lang != LangPython || view.AlreadyRunning {
		t.Errorf("view = %+v, unexpected", view)
	}
}

func TestStartAction_LangAliasesResolveToCanonical(t *testing.T) {
	cases := []struct {
		alias, want string
	}{
		{"py", LangPython}, {"python", LangPython},
		{"js", LangNode}, {"node", LangNode}, {"javascript", LangNode},
	}
	for _, tc := range cases {
		t.Run(tc.alias, func(t *testing.T) {
			home := shortTempDir(t)
			spawner := &stubSpawner{listen: true}
			var out bytes.Buffer
			code := startAction(
				[]string{"--name", "s", "--lang", tc.alias, "--bin", "sh"},
				home, []string{"/repo-" + tc.alias}, spawner.spawn(t), &out,
			)
			if code != int(ExitOK) {
				t.Fatalf("start --lang %s: exit = %d, want 0", tc.alias, code)
			}
			spec := spawner.lastSpec(t)
			if spec.Lang != tc.want {
				t.Errorf("spawned Lang = %q, want %q", spec.Lang, tc.want)
			}
		})
	}
}

func TestResolveLang_AcceptsAliasesRejectsUnknown(t *testing.T) {
	cases := map[string]string{
		"py": LangPython, "python": LangPython,
		"js": LangNode, "node": LangNode, "javascript": LangNode,
	}
	for alias, want := range cases {
		got, err := resolveLang(alias)
		if err != nil || got != want {
			t.Errorf("resolveLang(%q) = (%q, %v), want (%q, nil)", alias, got, err, want)
		}
	}
	if _, err := resolveLang("ruby"); err == nil {
		t.Error("resolveLang(\"ruby\") = nil error, want an error naming the valid languages")
	}
}

// --- eval -----------------------------------------------------------------

func echoCodeHandler(req Request) (Response, bool) {
	return Response{V: ProtocolVersion, OK: true, Value: req.Code}, true
}

func TestEvalAction_ArgumentWinsOverStdinAndDashDashDisambiguatesADash(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoCodeHandler)

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s", "positional-code"}, home, []string{root}, strings.NewReader("stdin-code"), &out)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "positional-code") || strings.Contains(out.String(), "stdin-code") {
		t.Errorf("output = %q, want the positional code, not stdin", out.String())
	}

	out.Reset()
	code = evalAction([]string{"--name", "s", "--", "-1 + 2"}, home, []string{root}, nil, &out)
	if code != int(ExitOK) {
		t.Fatalf("-- separator: exit = %d", code)
	}
	if !strings.Contains(out.String(), "-1 + 2") {
		t.Errorf("output = %q, want the dash-leading code echoed via --", out.String())
	}
}

func TestEvalAction_ReadsStdinWhenNoPositionalGiven(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoCodeHandler)

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s"}, home, []string{root}, strings.NewReader("piped-code"), &out)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "piped-code") {
		t.Errorf("output = %q, want the piped stdin code echoed", out.String())
	}
}

func TestEvalAction_UsageErrorWithNeitherArgumentNorStdin(t *testing.T) {
	home := shortTempDir(t)
	var out bytes.Buffer
	// nil stdin stands in for a live terminal — see isTerminalReader.
	code := evalAction([]string{"--name", "s"}, home, []string{"/repo"}, nil, &out)
	if code != int(ExitUsage) {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestEvalAction_SessionNotFoundExitsTwo(t *testing.T) {
	home := shortTempDir(t)
	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = evalAction([]string{"--name", "ghost", "1"}, home, []string{"/repo"}, nil, &out)
	})
	if code != int(ExitNotFound) {
		t.Errorf("exit = %d, want %d", code, ExitNotFound)
	}
	if !strings.Contains(stderr, "atomic repl start") {
		t.Errorf("stderr = %q, want it to name `atomic repl start` (reaped == never-started, uniform message)", stderr)
	}
}

func TestEvalAction_EvalExceptionExitsThree(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoHandler(Response{
		V: ProtocolVersion, OK: false, Error: "Traceback: boom",
	}))

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s", "raise Exception('boom')"}, home, []string{root}, nil, &out)
	if code != int(ExitEvalException) {
		t.Errorf("exit = %d, want %d", code, ExitEvalException)
	}
}

func TestEvalAction_TimeoutExitsFourAndRemovesSessionFiles(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	// PID left at zero: escalate returns immediately with no signal or grace
	// wait, keeping this test off the wall clock.
	sess := startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython, StartedAt: time.Now()}, func(Request) (Response, bool) {
		return Response{}, false // never replies — a runaway eval
	})

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s", "--timeout", "20ms", "while True: pass"}, home, []string{root}, nil, &out)
	if code != int(ExitTimeout) {
		t.Errorf("exit = %d, want %d", code, ExitTimeout)
	}
	if _, err := os.Stat(sess.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file still present after timeout: err=%v", err)
	}
	if _, err := os.Stat(sess.MetaPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("meta file still present after timeout: err=%v", err)
	}

	// The name now reads exactly like one that was never started.
	out.Reset()
	code = evalAction([]string{"--name", "s", "1"}, home, []string{root}, nil, &out)
	if code != int(ExitNotFound) {
		t.Errorf("post-timeout eval: exit = %d, want %d (not found)", code, ExitNotFound)
	}
}

func TestEvalAction_DeadSessionExitsFive(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	seedDeadSession(t, home, root, "s", Meta{Lang: LangPython})

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s", "1"}, home, []string{root}, nil, &out)
	if code != int(ExitDead) {
		t.Errorf("exit = %d, want %d", code, ExitDead)
	}
}

// Every verb that surfaces ExitDead must name `atomic repl start` as the fix,
// not report the session dead and leave the next step implicit.
func TestDeadSessionRemedy_NamesReplStart(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	seedDeadSession(t, home, root, "s", Meta{Lang: LangPython})

	var out bytes.Buffer
	for _, tc := range []struct {
		name string
		run  func() int
	}{
		{"eval", func() int {
			return evalAction([]string{"--name", "s", "1"}, home, []string{root}, nil, &out)
		}},
		{"status", func() int {
			return statusAction([]string{"--name", "s"}, home, []string{root}, &out)
		}},
		{"reset", func() int {
			return resetAction([]string{"--name", "s"}, home, []string{root}, &out)
		}},
		{"stop", func() int {
			return stopAction([]string{"--name", "s"}, home, []string{root}, &out)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			var code int
			stderr := captureStderr(t, func() {
				code = tc.run()
			})
			if code != int(ExitDead) {
				t.Errorf("exit = %d, want %d", code, ExitDead)
			}
			if !strings.Contains(stderr, "atomic repl start") {
				t.Errorf("stderr = %q, want it to name `atomic repl start`", stderr)
			}
		})
	}
}

// A socket can be gone while its meta is still on disk: `stop` returns on the
// harness's shutdown ack, and the harness clears socket and meta afterwards. A
// verb landing in that window finds the meta and dials nothing, so the error
// arrives from Dial rather than findSession. It must still read exactly like a
// name that was never started — that byte-identical reading is the promise
// notFoundError exists to keep, and the remedy is the part a reader acts on.
func TestNotFoundAfterSocketVanishes_ReadsLikeNeverStarted(t *testing.T) {
	root := "/repo"

	seeded := shortTempDir(t)
	seedSocketlessSession(t, seeded, root, "s", Meta{Lang: LangPython})
	// A home where "s" was never started at all, for the comparison.
	empty := shortTempDir(t)

	var out bytes.Buffer
	run := func(verb, home string) int {
		switch verb {
		case "eval":
			return evalAction([]string{"--name", "s", "1"}, home, []string{root}, nil, &out)
		case "status":
			return statusAction([]string{"--name", "s"}, home, []string{root}, &out)
		case "reset":
			return resetAction([]string{"--name", "s"}, home, []string{root}, &out)
		default:
			return stopAction([]string{"--name", "s"}, home, []string{root}, &out)
		}
	}

	for _, verb := range []string{"eval", "status", "reset", "stop"} {
		t.Run(verb, func(t *testing.T) {
			out.Reset()
			var code int
			stderr := captureStderr(t, func() { code = run(verb, seeded) })

			out.Reset()
			neverStarted := captureStderr(t, func() { run(verb, empty) })

			if code != int(ExitNotFound) {
				t.Errorf("exit = %d, want %d", code, ExitNotFound)
			}
			if !strings.Contains(stderr, "atomic repl start") {
				t.Errorf("stderr = %q, want it to name `atomic repl start`", stderr)
			}
			if strings.Contains(stderr, ".sock") {
				t.Errorf("stderr = %q, want no socket path — it leaks an internal detail the remedy replaces", stderr)
			}
			if stderr != neverStarted {
				t.Errorf("stderr = %q, want it byte-identical to the never-started reading %q", stderr, neverStarted)
			}
		})
	}
}

func TestEvalAction_ProtocolMismatchFailsLoudNamingStopThenStart(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoHandler(Response{
		V: ProtocolVersion + 1, OK: true,
	}))

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = evalAction([]string{"--name", "s", "1"}, home, []string{root}, nil, &out)
	})
	if code != int(ExitProtocolMismatch) {
		t.Errorf("exit = %d, want %d (distinct code for a protocol mismatch)", code, ExitProtocolMismatch)
	}
	if !strings.Contains(stderr, "repl stop") || !strings.Contains(stderr, "repl start") {
		t.Errorf("stderr = %q, want it to name `repl stop` then `repl start`", stderr)
	}
}

func TestEvalAction_JSONOutputMatchesResponseShape(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoHandler(Response{
		V: ProtocolVersion, OK: true, Value: "42", Stdout: "out\n",
	}))

	var out bytes.Buffer
	code := evalAction([]string{"--name", "s", "--json", "1+41"}, home, []string{root}, nil, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, out.String())
	}
	if resp.Value != "42" || resp.Stdout != "out\n" {
		t.Errorf("resp = %+v, unexpected", resp)
	}
}

// --- list -------------------------------------------------------------

func TestListAction_ExitsZeroEvenWithADeadEntry(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "alive-one", Meta{Lang: LangPython}, echoHandler(okResponse("")))
	seedDeadSession(t, home, root, "dead-one", Meta{Lang: LangNode})

	var out bytes.Buffer
	code := listAction(nil, home, []string{root}, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d, want 0 even with a dead entry", code)
	}
	if !strings.Contains(out.String(), "alive-one") || !strings.Contains(out.String(), "dead-one") {
		t.Errorf("output = %q, want both entries listed", out.String())
	}
	if !strings.Contains(out.String(), "dead") {
		t.Errorf("output = %q, want the dead entry marked dead", out.String())
	}
}

func TestListAction_AllEnumeratesAcrossScopesWithFields(t *testing.T) {
	home := shortTempDir(t)
	startStubSessionAt(t, home, "/repo-a", "s", Meta{Lang: LangPython, PID: 4242}, echoHandler(okResponse("")))
	startStubSessionAt(t, home, "/repo-b", "s", Meta{Lang: LangNode, PID: 4343}, echoHandler(okResponse("")))

	var out bytes.Buffer
	// scopeRoots deliberately names neither scope — --all must still find them.
	code := listAction([]string{"--all", "--json"}, home, []string{"/unrelated"}, &out)
	if code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var views []sessionView
	if err := json.Unmarshal(out.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v; body=%s", err, out.String())
	}
	if len(views) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(views), views)
	}
	roots := map[string]bool{}
	for _, v := range views {
		roots[v.Root] = true
		if v.Name == "" || v.Lang == "" || v.PID == 0 {
			t.Errorf("view %+v: missing name/lang/pid", v)
		}
		if !v.Alive {
			t.Errorf("view %+v: want alive (a live stub is serving)", v)
		}
	}
	if !roots["/repo-a"] || !roots["/repo-b"] {
		t.Errorf("roots seen = %v, want both /repo-a and /repo-b", roots)
	}
}

func TestListAndStatus_NeverIncludeEnvValues(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	envDir := shortTempDir(t)
	envFile := filepath.Join(envDir, ".env")
	secret := "SUPER_SECRET_TOKEN_VALUE_12345"
	if err := os.WriteFile(envFile, []byte("TOKEN="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	spawner := &stubSpawner{listen: true}
	var startOut bytes.Buffer
	code := startAction(
		[]string{"--name", "s", "--lang", "py", "--bin", "sh", "--env", envFile},
		home, []string{root}, spawner.spawn(t), &startOut,
	)
	if code != int(ExitOK) {
		t.Fatalf("start: exit = %d", code)
	}
	spec := spawner.lastSpec(t)
	if !containsEntry(spec.Env, "TOKEN="+secret) {
		t.Fatalf("spawned env did not carry TOKEN — test setup is broken: %v", spec.Env)
	}

	var listOut, statusOut bytes.Buffer
	if code := listAction([]string{"--json"}, home, []string{root}, &listOut); code != int(ExitOK) {
		t.Fatalf("list: exit = %d", code)
	}
	if code := statusAction([]string{"--name", "s", "--json"}, home, []string{root}, &statusOut); code != int(ExitOK) {
		t.Fatalf("status: exit = %d", code)
	}
	if strings.Contains(listOut.String(), secret) {
		t.Errorf("list output leaked the --env secret: %s", listOut.String())
	}
	if strings.Contains(statusOut.String(), secret) {
		t.Errorf("status output leaked the --env secret: %s", statusOut.String())
	}
}

// --- status -----------------------------------------------------------

func TestStatusAction_ReportsLiveSessionAndExitsFiveWhenDead(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "alive", Meta{Lang: LangPython, PID: 111}, echoHandler(okResponse("")))
	seedDeadSession(t, home, root, "dead", Meta{Lang: LangNode, PID: 222})

	var out bytes.Buffer
	if code := statusAction([]string{"--name", "alive"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Errorf("alive: exit = %d, want 0", code)
	}
	out.Reset()
	if code := statusAction([]string{"--name", "dead"}, home, []string{root}, &out); code != int(ExitDead) {
		t.Errorf("dead: exit = %d, want %d", code, ExitDead)
	}
	out.Reset()
	if code := statusAction([]string{"--name", "ghost"}, home, []string{root}, &out); code != int(ExitNotFound) {
		t.Errorf("ghost: exit = %d, want %d", code, ExitNotFound)
	}
}

func TestStatusAction_AllSearchesEveryScope(t *testing.T) {
	home := shortTempDir(t)
	startStubSessionAt(t, home, "/repo-elsewhere", "s", Meta{Lang: LangPython}, echoHandler(okResponse("")))

	var out bytes.Buffer
	if code := statusAction([]string{"--name", "s"}, home, []string{"/repo-current"}, &out); code != int(ExitNotFound) {
		t.Fatalf("without --all: exit = %d, want %d", code, ExitNotFound)
	}
	out.Reset()
	if code := statusAction([]string{"--name", "s", "--all"}, home, []string{"/repo-current"}, &out); code != int(ExitOK) {
		t.Errorf("with --all: exit = %d, want 0", code)
	}
}

func TestStatusAction_JSONOutput(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython, PID: 42}, echoHandler(okResponse("")))

	var out bytes.Buffer
	if code := statusAction([]string{"--name", "s", "--json"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var view sessionView
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v; body=%s", err, out.String())
	}
	if view.Name != "s" || !view.Alive {
		t.Errorf("view = %+v, unexpected", view)
	}
}

// --- reset / stop -------------------------------------------------------

func TestResetAction_SendsResetOp(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	var gotOp string
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, func(req Request) (Response, bool) {
		gotOp = req.Op
		return Response{V: ProtocolVersion, OK: true}, true
	})

	var out bytes.Buffer
	if code := resetAction([]string{"--name", "s"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	if gotOp != OpReset {
		t.Errorf("op sent = %q, want %q", gotOp, OpReset)
	}
}

func TestResetAction_JSONOutput(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoHandler(Response{V: ProtocolVersion, OK: true}))

	var out bytes.Buffer
	if code := resetAction([]string{"--name", "s", "--json"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, out.String())
	}
	if !resp.OK {
		t.Errorf("resp.OK = false, want true")
	}
}

func TestStopAction_SendsShutdownOp(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	var gotOp string
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, func(req Request) (Response, bool) {
		gotOp = req.Op
		return Response{V: ProtocolVersion, OK: true}, true
	})

	var out bytes.Buffer
	if code := stopAction([]string{"--name", "s"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	if gotOp != OpShutdown {
		t.Errorf("op sent = %q, want %q", gotOp, OpShutdown)
	}
}

func TestStopAction_JSONOutput(t *testing.T) {
	home := shortTempDir(t)
	root := "/repo"
	startStubSessionAt(t, home, root, "s", Meta{Lang: LangPython}, echoHandler(Response{V: ProtocolVersion, OK: true}))

	var out bytes.Buffer
	if code := stopAction([]string{"--name", "s", "--json"}, home, []string{root}, &out); code != int(ExitOK) {
		t.Fatalf("exit = %d", code)
	}
	var resp Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, out.String())
	}
	if !resp.OK {
		t.Errorf("resp.OK = false, want true")
	}
}

func TestResetAndStopAction_SessionNotFoundExitsTwo(t *testing.T) {
	home := shortTempDir(t)
	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = resetAction([]string{"--name", "ghost"}, home, []string{"/repo"}, &out)
	})
	if code != int(ExitNotFound) {
		t.Errorf("reset: exit = %d, want %d", code, ExitNotFound)
	}
	if !strings.Contains(stderr, "atomic repl start") {
		t.Errorf("reset: stderr = %q, want it to name `atomic repl start`", stderr)
	}

	out.Reset()
	stderr = captureStderr(t, func() {
		code = stopAction([]string{"--name", "ghost"}, home, []string{"/repo"}, &out)
	})
	if code != int(ExitNotFound) {
		t.Errorf("stop: exit = %d, want %d", code, ExitNotFound)
	}
	if !strings.Contains(stderr, "atomic repl start") {
		t.Errorf("stop: stderr = %q, want it to name `atomic repl start`", stderr)
	}
}

// --- scope resolution / realm visibility -------------------------------

// mustMarkScope writes a scope marker via the production primitive rather than
// hand-writing atomic.toml, so this test tracks the real marker format.
func mustMarkScope(t *testing.T, root, scope string) {
	t.Helper()
	if _, err := config.EnsureScopeMarker(root, scope); err != nil {
		t.Fatalf("mark %s as scope=%s: %v", root, scope, err)
	}
}

func TestResolveScopeRoots_RealmMembershipViaScopeMarkerWalk(t *testing.T) {
	base := shortTempDir(t)
	realm := filepath.Join(base, "realm")
	memberA := filepath.Join(realm, "member-a")
	memberB := filepath.Join(realm, "member-b")
	for _, d := range []string{memberA, memberB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	mustMarkScope(t, realm, "realm")
	mustMarkScope(t, memberA, "repo")
	mustMarkScope(t, memberB, "repo")

	rootsA, err := resolveScopeRoots(memberA, "")
	if err != nil {
		t.Fatalf("resolveScopeRoots(memberA): %v", err)
	}
	if len(rootsA) != 2 || rootsA[0] != memberA || rootsA[1] != realm {
		t.Errorf("resolveScopeRoots(memberA) = %v, want [%s %s]", rootsA, memberA, realm)
	}

	rootsRealm, err := resolveScopeRoots(realm, "")
	if err != nil {
		t.Fatalf("resolveScopeRoots(realm): %v", err)
	}
	if len(rootsRealm) != 1 || rootsRealm[0] != realm {
		t.Errorf("resolveScopeRoots(realm) = %v, want [%s] (invoked directly at a realm root collapses to one entry)", rootsRealm, realm)
	}
}

func TestReplAction_RealmSessionVisibleFromMemberButSiblingSessionIsNot(t *testing.T) {
	home := shortTempDir(t)
	base := shortTempDir(t)
	realm := filepath.Join(base, "realm")
	memberA := filepath.Join(realm, "member-a")
	memberB := filepath.Join(realm, "member-b")
	for _, d := range []string{memberA, memberB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	mustMarkScope(t, realm, "realm")
	mustMarkScope(t, memberA, "repo")
	mustMarkScope(t, memberB, "repo")

	startStubSessionAt(t, home, realm, "shared", Meta{Lang: LangPython}, echoCodeHandler)
	startStubSessionAt(t, home, memberA, "local-a", Meta{Lang: LangPython}, echoCodeHandler)

	var out bytes.Buffer
	// From member-a: both the realm session and its own local session are visible.
	if code := ReplAction([]string{"eval", "--name", "shared", "hi"}, home, memberA, "", nil, &out); code != int(ExitOK) {
		t.Errorf("eval shared from member-a: exit = %d, want 0", code)
	}
	out.Reset()
	if code := ReplAction([]string{"eval", "--name", "local-a", "hi"}, home, memberA, "", nil, &out); code != int(ExitOK) {
		t.Errorf("eval local-a from member-a: exit = %d, want 0", code)
	}

	// From member-b: the realm session is still visible, but member-a's own
	// session must stay invisible.
	out.Reset()
	if code := ReplAction([]string{"eval", "--name", "shared", "hi"}, home, memberB, "", nil, &out); code != int(ExitOK) {
		t.Errorf("eval shared from member-b: exit = %d, want 0", code)
	}
	out.Reset()
	if code := ReplAction([]string{"eval", "--name", "local-a", "hi"}, home, memberB, "", nil, &out); code != int(ExitNotFound) {
		t.Errorf("eval local-a from member-b: exit = %d, want %d (not found — a sibling's local session is invisible)", code, ExitNotFound)
	}

	// list mirrors eval's visibility: from member-a both names appear, from
	// member-b only the realm-shared one.
	out.Reset()
	if code := ReplAction([]string{"list"}, home, memberA, "", nil, &out); code != int(ExitOK) {
		t.Errorf("list from member-a: exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "shared") || !strings.Contains(out.String(), "local-a") {
		t.Errorf("list from member-a = %q, want both shared and local-a", out.String())
	}

	out.Reset()
	if code := ReplAction([]string{"list"}, home, memberB, "", nil, &out); code != int(ExitOK) {
		t.Errorf("list from member-b: exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "shared") {
		t.Errorf("list from member-b = %q, want the realm-shared session", out.String())
	}
	if strings.Contains(out.String(), "local-a") {
		t.Errorf("list from member-b = %q, want member-a's local session absent", out.String())
	}
}

// --- resolveIdleTimeout ([repl] idle_timeout config) --------------

// writeRepoReplConfig writes content to <scopeRoot>/.claude/atomic.toml.
func writeRepoReplConfig(t *testing.T, scopeRoot, content string) {
	t.Helper()
	dir := filepath.Join(scopeRoot, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write repo config: %v", err)
	}
}

// writeUserReplConfig writes content to <home>/.atomic/config.toml.
func writeUserReplConfig(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir user config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write user config: %v", err)
	}
}

// Neither config carries [repl] idle_timeout — falls back to DefaultIdleTimeout.
func TestResolveIdleTimeout_BothAbsentDefaultsToOneHour(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)

	if got, _ := resolveIdleTimeout(home, scopeRoot); got != DefaultIdleTimeout {
		t.Errorf("resolveIdleTimeout = %v, want %v (default)", got, DefaultIdleTimeout)
	}
}

// The user-level idle_timeout applies when the repo config has none.
func TestResolveIdleTimeout_UserAppliesWhenRepoAbsent(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeUserReplConfig(t, home, "[repl]\nidle_timeout = \"2h\"\n")

	want := 2 * time.Hour
	if got, _ := resolveIdleTimeout(home, scopeRoot); got != want {
		t.Errorf("resolveIdleTimeout = %v, want %v", got, want)
	}
}

// A repo-level idle_timeout takes precedence over a user-level one.
func TestResolveIdleTimeout_RepoWinsOverUser(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeUserReplConfig(t, home, "[repl]\nidle_timeout = \"2h\"\n")
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"30m\"\n")

	want := 30 * time.Minute
	if got, _ := resolveIdleTimeout(home, scopeRoot); got != want {
		t.Errorf("resolveIdleTimeout = %v, want %v", got, want)
	}
}

// A malformed repo idle_timeout is skipped in favor of a valid user-level one
// rather than blocking session start — doctor is what surfaces the bad value.
func TestResolveIdleTimeout_InvalidRepoFallsThroughToUser(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeUserReplConfig(t, home, "[repl]\nidle_timeout = \"90s\"\n")
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"bogus\"\n")

	want := 90 * time.Second
	if got, _ := resolveIdleTimeout(home, scopeRoot); got != want {
		t.Errorf("resolveIdleTimeout = %v, want %v", got, want)
	}
}

// Malformed or non-positive values at both scopes fall through to the default.
func TestResolveIdleTimeout_InvalidBothFallsThroughToDefault(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeUserReplConfig(t, home, "[repl]\nidle_timeout = \"0s\"\n")
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"-5m\"\n")

	if got, _ := resolveIdleTimeout(home, scopeRoot); got != DefaultIdleTimeout {
		t.Errorf("resolveIdleTimeout = %v, want %v (default)", got, DefaultIdleTimeout)
	}
}

// --- invalid idle_timeout is surfaced at start time ---

// resolveIdleTimeout degrades quietly past a bad value, which without this
// warning leaves a mistyped idle_timeout in effect indefinitely with nothing but
// `atomic doctor` to reveal it. The run proceeds; the use site says so out loud.
func TestStartAction_InvalidIdleTimeoutWarnsNamingFileAndValue(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"bogus\"\n")

	spawner := &stubSpawner{listen: true}
	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = startAction(
			[]string{"--name", "s", "--lang", "py", "--bin", "sh"},
			home, []string{scopeRoot}, spawner.spawn(t), &out,
		)
	})

	if code != int(ExitOK) {
		t.Errorf("exit = %d, want %d — an invalid idle_timeout warns, it never blocks a start", code, ExitOK)
	}
	wantPath := config.RepoConfigPath(scopeRoot)
	if !strings.Contains(stderr, wantPath) {
		t.Errorf("stderr = %q, want it to name the config file %q", stderr, wantPath)
	}
	if !strings.Contains(stderr, `"bogus"`) {
		t.Errorf("stderr = %q, want it to name the invalid value", stderr)
	}
}

// Repo and user configs each carrying a bad value produce one line each — an
// agent reading stderr has to be able to tell which file to fix.
func TestStartAction_InvalidIdleTimeoutWarnsOncePerScope(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"bogus\"\n")
	writeUserReplConfig(t, home, "[repl]\nidle_timeout = \"-5m\"\n")

	spawner := &stubSpawner{listen: true}
	var out bytes.Buffer
	stderr := captureStderr(t, func() {
		startAction(
			[]string{"--name", "s", "--lang", "py", "--bin", "sh"},
			home, []string{scopeRoot}, spawner.spawn(t), &out,
		)
	})

	if got := strings.Count(stderr, "idle_timeout"); got != 2 {
		t.Errorf("idle_timeout mentions = %d, want 2 (one per invalid scope); stderr = %q", got, stderr)
	}
	if !strings.Contains(stderr, config.RepoConfigPath(scopeRoot)) {
		t.Errorf("stderr = %q, want the repo config path", stderr)
	}
	if !strings.Contains(stderr, config.TOMLPath(home)) {
		t.Errorf("stderr = %q, want the user config path", stderr)
	}
}

// The warning is a diagnostic, not a running commentary: a well-formed config
// says nothing.
func TestStartAction_ValidIdleTimeoutIsSilent(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"30m\"\n")

	spawner := &stubSpawner{listen: true}
	var out bytes.Buffer
	stderr := captureStderr(t, func() {
		startAction(
			[]string{"--name", "s", "--lang", "py", "--bin", "sh"},
			home, []string{scopeRoot}, spawner.spawn(t), &out,
		)
	})
	if stderr != "" {
		t.Errorf("stderr = %q, want empty for a valid idle_timeout", stderr)
	}
}

// The window is resolved once, when a session is spawned. Repeating the warning
// on every eval would put it in front of the code's own output, which is where
// an agent reads results — start is the one place it is actionable.
func TestEvalAction_InvalidIdleTimeoutIsSilent(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	home := shortTempDir(t)
	scopeRoot := shortTempDir(t)
	writeRepoReplConfig(t, scopeRoot, "[repl]\nidle_timeout = \"bogus\"\n")
	startStubSessionAt(t, home, scopeRoot, "s", Meta{Lang: LangPython}, func(Request) (Response, bool) {
		return Response{V: ProtocolVersion, OK: true, Value: "2"}, true
	})

	var out bytes.Buffer
	var code int
	stderr := captureStderr(t, func() {
		code = evalAction([]string{"--name", "s", "1 + 1"}, home, []string{scopeRoot}, nil, &out)
	})
	if code != int(ExitOK) {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	if strings.Contains(stderr, "idle_timeout") {
		t.Errorf("stderr = %q, want no idle_timeout warning on eval", stderr)
	}
}

// The one duplicated fact between this package and internal/config: repl owns the
// concrete fallback, config owns the string `atomic config list` shows for an
// unset repl.idle_timeout. config cannot import repl (that would cycle), so the
// two are kept in sync by hand and nothing else fails when they drift. An agent
// told "1h" while sessions reap on some other window has been lied to.
func TestDefaultIdleTimeout_MatchesConfigDisplayDefault(t *testing.T) {
	display, ok := config.Resolved(config.Default())["repl.idle_timeout"]
	if !ok {
		t.Fatal("config.Resolved has no repl.idle_timeout key")
	}
	shown, err := config.ValidateIdleTimeout(display)
	if err != nil {
		t.Fatalf("config's displayed default %q does not parse: %v", display, err)
	}
	if shown != DefaultIdleTimeout {
		t.Errorf("config displays repl.idle_timeout default %q (%v), repl uses %v — the two have drifted",
			display, shown, DefaultIdleTimeout)
	}
}

// containsEntry is defined in spawn_test.go (same package); reused here.
