package repl

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// ErrInterpreterUnavailable marks the one start failure that is not the
// caller's mistake: --lang's interpreter is not installed, or an explicit --bin
// does not resolve. The CLI maps it to its own exit code so an agent can tell
// "install it, or point --bin somewhere real" apart from "I wrote the command
// wrong".
var ErrInterpreterUnavailable = errors.New("repl: interpreter unavailable")

// DefaultIdleTimeout is the window a session self-terminates after when no
// config supplies one.
const DefaultIdleTimeout = time.Hour

const (
	// defaultStartWait bounds how long `start` waits for a freshly spawned
	// harness to bind. An interpreter that has not accepted a connection by
	// now is not coming up.
	defaultStartWait = 10 * time.Second
	// defaultStartPoll is how often that wait re-probes.
	defaultStartPoll = 25 * time.Millisecond
	// liveProbeTimeout bounds one liveness dial. A unix connect is local and
	// immediate; anything slower is a wedged peer, not a slow one.
	liveProbeTimeout = 2 * time.Second
)

// defaultBins maps a canonical language to the interpreter looked up on PATH
// when --bin is not given.
var defaultBins = map[string]string{
	LangPython: "python3",
	LangNode:   "node",
}

// SpawnSpec is everything one harness process needs, resolved. It is passed
// whole rather than as loose arguments so the injected SpawnFunc in tests sees
// exactly what the real spawn would have used.
type SpawnSpec struct {
	Lang       string
	Bin        string // absolute, already resolved through PATH
	ScriptPath string // the materialized harness script
	SocketPath string
	MetaPath   string
	ScopeRoot  string // the harness's working directory

	IdleTimeout time.Duration
	Env         []string // the child's whole environment, not an overlay
}

// SpawnFunc starts a harness and returns its pid. It is a seam for the same
// reason codeintel/mcp's is: the concurrency and failure paths above it are
// worth testing on every machine, and a real interpreter is neither guaranteed
// present nor free to leak.
type SpawnFunc func(spec SpawnSpec) (pid int, err error)

// DefaultSpawn starts the interpreter against the materialized harness,
// detached, and returns its pid.
//
// Setsid puts the harness in its own session so it outlives the CLI invocation
// that started it — the whole point of a persistent session. Its standard
// streams are nil (/dev/null): nothing reads them, and an inherited pipe would
// block the harness once its buffer filled.
func DefaultSpawn(spec SpawnSpec) (int, error) {
	cmd := exec.Command(spec.Bin, spec.ScriptPath,
		"--socket", spec.SocketPath,
		"--idle-timeout", formatIdleTimeout(spec.IdleTimeout),
		"--meta", spec.MetaPath)
	cmd.Dir = spec.ScopeRoot
	cmd.Env = spec.Env
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("repl: start %s: %w", spec.Bin, err)
	}
	pid := cmd.Process.Pid
	// Nothing here will ever Wait: the harness outlives this process, and
	// releasing the handle hands reaping to init.
	_ = cmd.Process.Release()
	return pid, nil
}

// formatIdleTimeout renders the window as decimal seconds, which is what both
// harnesses parse (a float, not a Go duration string).
func formatIdleTimeout(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// IsLive reports whether a session is serving at socketPath. It dials rather
// than stats: a crashed harness leaves its socket file behind, so file
// existence proves nothing, and treating it as proof is how a dead session gets
// reported as alive.
func IsLive(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, liveProbeTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ResolveInterpreter resolves the binary to spawn for lang, honoring an
// explicit override. Both paths go through exec.LookPath, so an override that
// does not exist fails here rather than as an opaque exec error after the
// session directory has been built.
func ResolveInterpreter(lang, override string) (string, error) {
	name := override
	if name == "" {
		var ok bool
		name, ok = defaultBins[lang]
		if !ok {
			return "", unknownLangError(lang)
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrInterpreterUnavailable, name, err)
	}
	return path, nil
}

// StartOptions describes one `repl start`.
type StartOptions struct {
	Home      string // injected, never os.UserHomeDir
	ScopeRoot string // the repo or realm root this session keys to
	Name      string
	Lang      string // canonical: LangPython or LangNode
	Bin       string // optional --bin override

	// Env is the extra KEY=VALUE set (typically from --env) layered over this
	// process's own environment.
	Env []string

	IdleTimeout time.Duration

	Spawn        SpawnFunc // nil uses DefaultSpawn
	WaitTimeout  time.Duration
	PollInterval time.Duration
}

// EnsureStarted guarantees a live session for opts, returning its meta and
// whether it was already running.
//
// The probe, the stale-socket cleanup, and the spawn are one decision taken
// under one flock. Guarding only the spawn call would still let two callers
// both observe "dead" first and both spawn — the second one binding over the
// first's socket and orphaning a process holding live state. A caller that
// loses the race blocks, wakes to a live session, and reports already-running.
func EnsureStarted(opts StartOptions) (Meta, bool, error) {
	sockPath, err := SocketPath(opts.Home, opts.ScopeRoot, opts.Name)
	if err != nil {
		return Meta{}, false, err
	}
	metaPath, err := MetaPath(opts.Home, opts.ScopeRoot, opts.Name)
	if err != nil {
		return Meta{}, false, err
	}
	lockPath, err := LockPath(opts.Home, opts.ScopeRoot, opts.Name)
	if err != nil {
		return Meta{}, false, err
	}

	// Before anything is created: an absent interpreter is reported as such,
	// not as a session that failed to come up for unclear reasons.
	bin, err := ResolveInterpreter(opts.Lang, opts.Bin)
	if err != nil {
		return Meta{}, false, err
	}

	if IsLive(sockPath) {
		return describeLiveSession(metaPath, opts, sockPath), true, nil
	}

	dir := SessionDir(opts.Home, opts.ScopeRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Meta{}, false, fmt.Errorf("repl: create session dir %s: %w", dir, err)
	}

	lock, err := acquireSessionLock(lockPath)
	if err != nil {
		return Meta{}, false, err
	}
	defer lock.release()

	// Re-probe under the lock: a racer may have won while this call blocked.
	if IsLive(sockPath) {
		return describeLiveSession(metaPath, opts, sockPath), true, nil
	}

	// Nothing is listening, so whatever is at the path is debris from a
	// harness that died without cleaning up — and bind refuses a path that
	// already exists.
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return Meta{}, false, fmt.Errorf("repl: remove stale socket %s: %w", sockPath, err)
	}

	scriptPath, err := materializeHarness(dir, opts.Lang)
	if err != nil {
		return Meta{}, false, err
	}

	idle := opts.IdleTimeout
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	spawn := opts.Spawn
	if spawn == nil {
		spawn = DefaultSpawn
	}

	startedAt := time.Now()
	pid, err := spawn(SpawnSpec{
		Lang:        opts.Lang,
		Bin:         bin,
		ScriptPath:  scriptPath,
		SocketPath:  sockPath,
		MetaPath:    metaPath,
		ScopeRoot:   opts.ScopeRoot,
		IdleTimeout: idle,
		Env:         append(os.Environ(), opts.Env...),
	})
	if err != nil {
		return Meta{}, false, fmt.Errorf("repl: spawn %s harness: %w", opts.Lang, err)
	}

	if err := waitLive(sockPath, opts.WaitTimeout, opts.PollInterval); err != nil {
		// The pid is deliberately not signaled here. A harness that failed to
		// bind has almost always already exited, and a start path that kills
		// by a just-obtained pid is one race away from killing something else;
		// a harness that is somehow still up carries its own idle window and
		// retires itself. No meta is written, so the session reads as
		// never-started rather than pointing at a pid serving nothing.
		return Meta{}, false, fmt.Errorf("%w (harness pid %d)", err, pid)
	}

	meta := Meta{
		Name:      opts.Name,
		Lang:      opts.Lang,
		Bin:       bin,
		PID:       pid,
		Socket:    sockPath,
		StartedAt: startedAt,
		Root:      opts.ScopeRoot,
	}
	if err := meta.Save(metaPath); err != nil {
		return Meta{}, false, err
	}
	return meta, false, nil
}

// describeLiveSession answers an already-running start from the meta on disk.
// A live socket with unreadable meta is still a live session — the caller is
// told it is running, with the fields that could be recovered, rather than
// handed an error about a file that is not what it asked about.
func describeLiveSession(metaPath string, opts StartOptions, sockPath string) Meta {
	if meta, err := LoadMeta(metaPath); err == nil {
		return meta
	}
	return Meta{Name: opts.Name, Lang: opts.Lang, Socket: sockPath, Root: opts.ScopeRoot}
}

// waitLive polls until the socket accepts a connection or the bound elapses.
func waitLive(sockPath string, timeout, poll time.Duration) error {
	if timeout <= 0 {
		timeout = defaultStartWait
	}
	if poll <= 0 {
		poll = defaultStartPoll
	}
	deadline := time.Now().Add(timeout)
	for {
		if IsLive(sockPath) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("repl: harness socket %s did not accept a connection within %s of spawning", sockPath, timeout)
		}
		time.Sleep(poll)
	}
}

// materializeHarness writes the embedded harness for lang into dir and returns
// its path.
//
// It rewrites unconditionally, through a temp file and a rename: the script on
// disk is a cache of bytes compiled into this binary, and after an `atomic
// update` (or a hand edit) a stale copy is a harness speaking a protocol this
// client no longer does. The rename keeps a concurrent start from ever
// executing a half-written file.
func materializeHarness(dir, lang string) (string, error) {
	name, err := HarnessFilename(lang)
	if err != nil {
		return "", err
	}
	script, err := HarnessScript(lang)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("repl: create harness dir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".harness-*.tmp")
	if err != nil {
		return "", fmt.Errorf("repl: create temp harness in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o700); err != nil {
		tmp.Close()
		return "", fmt.Errorf("repl: chmod temp harness %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(script); err != nil {
		tmp.Close()
		return "", fmt.Errorf("repl: write temp harness %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("repl: close temp harness %s: %w", tmpPath, err)
	}

	path := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("repl: install harness %s: %w", path, err)
	}
	return path, nil
}

// sessionLock is an exclusive flock over one session's lock file.
type sessionLock struct {
	file *os.File
}

func acquireSessionLock(path string) (*sessionLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("repl: open session lock %s: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("repl: lock %s: %w", path, err)
	}
	return &sessionLock{file: file}, nil
}

func (l *sessionLock) release() {
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
}
