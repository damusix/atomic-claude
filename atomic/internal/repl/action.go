package repl

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	charmterm "github.com/charmbracelet/x/term"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
)

// ReplAction is the exported entry point for `atomic repl`. home and cwd are
// injected rather than resolved here, so cmd/atomic/main.go owns the one
// os.UserHomeDir/os.Getwd call and every path stays testable against a temp
// home. stdin flows only to eval, the one verb that reads it.
func ReplAction(args []string, home, cwd, repoOverride string, stdin io.Reader, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic repl <start|eval|list|status|reset|stop> [flags]")
		return int(ExitUsage)
	}

	scopeRoots, err := resolveScopeRoots(cwd, repoOverride)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl: %v\n", err)
		return int(ExitUsage)
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "start":
		// spawn nil means DefaultSpawn: a real interpreter is what production
		// start means. Tests call startAction directly with a stub SpawnFunc.
		return startAction(rest, home, scopeRoots, nil, out)
	case "eval":
		return evalAction(rest, home, scopeRoots, stdin, out)
	case "list":
		return listAction(rest, home, scopeRoots, out)
	case "status":
		return statusAction(rest, home, scopeRoots, out)
	case "reset":
		return resetAction(rest, home, scopeRoots, out)
	case "stop":
		return stopAction(rest, home, scopeRoots, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic repl: unknown verb %q\n", verb)
		return int(ExitUsage)
	}
}

// resolveScopeRoots returns the calling repo's scope root plus its enclosing
// realm's when the repo is a realm member — the union the read verbs search, and
// (first element) the root start keys a new session to. Realm membership comes
// from the scope-marker walk, never the <wikis> registry: that registry is a
// per-user preference, not a fact about this directory tree, and visibility must
// not depend on whose machine it runs on.
//
// Invoked at a realm root, repoctx finds no repo marker and falls back to dir
// itself, so both roots converge and the union collapses to one entry.
func resolveScopeRoots(dir, repoOverride string) ([]string, error) {
	repoRoot, _, err := repoctx.ResolveFrom(dir, repoOverride)
	if err != nil {
		return nil, err
	}

	realmSearchDir := dir
	if repoOverride != "" {
		// An explicit --repo redirects everything downstream of it, including
		// where the enclosing realm is searched for.
		realmSearchDir = repoRoot
	}

	roots := []string{repoRoot}
	if realmRoot, found := config.FindScopeRoot(realmSearchDir, "realm"); found && realmRoot != repoRoot {
		roots = append(roots, realmRoot)
	}
	return roots, nil
}

// resolveIdleTimeout resolves a new session's idle window: repo config first,
// then user config, else DefaultIdleTimeout. An unreadable file or an invalid
// duration falls through to the next tier rather than blocking a start.
//
// Degrading is not the same as staying silent. A present but invalid value
// yields a warning naming its file and value, returned for the caller to print:
// doctor flags it too, but nobody runs doctor because a session was reaped on a
// window they thought they had changed. An absent value warns about nothing.
func resolveIdleTimeout(home, scopeRoot string) (time.Duration, []string) {
	var warnings []string

	repoPath := config.RepoConfigPath(scopeRoot)
	if repoCfg, _, err := config.LoadRepoConfig(repoPath); err == nil {
		d, verr := config.ValidateIdleTimeout(repoCfg.Repl.IdleTimeout)
		switch {
		case verr == nil:
			return d, warnings
		case repoCfg.Repl.IdleTimeout != "":
			warnings = append(warnings, fmt.Sprintf("%s: %v; ignored", repoPath, verr))
		}
	}

	userPath := config.TOMLPath(home)
	if userCfg, _, err := config.Load(userPath); err == nil {
		d, verr := config.ValidateIdleTimeout(userCfg.Repl.IdleTimeout)
		switch {
		case verr == nil:
			return d, warnings
		case userCfg.Repl.IdleTimeout != "":
			warnings = append(warnings, fmt.Sprintf("%s: %v; ignored", userPath, verr))
		}
	}

	return DefaultIdleTimeout, warnings
}

// langAliases maps every accepted --lang spelling to its canonical form. py/js
// stay canonical; the long forms exist so an agent need not recall the short.
var langAliases = map[string]string{
	LangPython:   LangPython,
	"python":     LangPython,
	LangNode:     LangNode,
	"node":       LangNode,
	"javascript": LangNode,
}

// resolveLang canonicalizes a --lang value, or reports every accepted spelling.
func resolveLang(raw string) (string, error) {
	canon, ok := langAliases[raw]
	if !ok {
		return "", fmt.Errorf("repl: unknown --lang %q; valid: py (alias: python), js (aliases: node, javascript)", raw)
	}
	return canon, nil
}

// exitCodeForErr is the one place a package error becomes a process exit code,
// so every verb routes the same error the same way.
func exitCodeForErr(err error) ExitCode {
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return ExitNotFound
	case errors.Is(err, ErrSessionDead):
		return ExitDead
	case errors.Is(err, ErrEvalTimeout):
		return ExitTimeout
	case errors.Is(err, ErrInterpreterUnavailable):
		return ExitInterpreterUnavailable
	}
	var mismatch *ProtocolMismatchError
	if errors.As(err, &mismatch) {
		return ExitProtocolMismatch
	}
	return ExitUsage
}

// deadSessionError adds the `atomic repl start` remedy to a dead-session error.
// ErrSessionDead surfaces from more than one call site, so this wraps where each
// verb turns the error into stderr text. A non-dead error passes through.
func deadSessionError(err error) error {
	if !errors.Is(err, ErrSessionDead) {
		return err
	}
	return fmt.Errorf("%w; run `atomic repl start` to replace it", err)
}

// dialError renders an error from a dial that findSession already approved, where
// two of them need text the raw error cannot carry.
//
// Not-found here is not a name nobody started: the meta was on disk a moment ago.
// It is the window `stop` opens by returning on the harness's shutdown ack while
// the harness is still removing its own socket and meta. Dial reports that as the
// socket path with no remedy, so it is rebuilt through notFoundError — the
// session is gone either way, and the reader is owed the same sentence.
//
// Dead sessions keep the replace-it remedy they already had.
func dialError(err error, name string) error {
	if errors.Is(err, ErrSessionNotFound) {
		return notFoundError(name)
	}
	return deadSessionError(err)
}

// sessionView is the shape list and status share: enough to find a session and
// judge whether it can still be used. There is deliberately no --env field —
// Meta carries none either — so there is nowhere for a secret to leak.
type sessionView struct {
	Name      string    `json:"name"`
	Root      string    `json:"root"`
	Lang      string    `json:"lang"`
	Bin       string    `json:"bin"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Alive     bool      `json:"alive"`
}

func metaToView(m Meta, alive bool) sessionView {
	return sessionView{
		Name: m.Name, Root: m.Root, Lang: m.Lang, Bin: m.Bin,
		PID: m.PID, StartedAt: m.StartedAt, Alive: alive,
	}
}

// startView is start's --json shape: a sessionView plus whether this call found
// the session already running rather than spawning it.
type startView struct {
	sessionView
	AlreadyRunning bool `json:"already_running"`
}

// emitJSON encodes v to out as the command's --json output.
func emitJSON(out io.Writer, v any) int {
	if err := json.NewEncoder(out).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "repl: encode JSON: %v\n", err)
		return int(ExitUsage)
	}
	return int(ExitOK)
}

func livenessLabel(alive bool) string {
	if alive {
		return "alive"
	}
	return "dead"
}

// notFoundError is the one place the not-found message is built, so a reaped
// session and a name never started produce byte-identical text — see
// ErrSessionNotFound for why there is no marker telling them apart.
func notFoundError(name string) error {
	return fmt.Errorf("%w: %q; run `atomic repl start --name %s --lang <py|js>` to create it", ErrSessionNotFound, name, name)
}

// findSession resolves name against each root in turn — the calling scope first,
// then its enclosing realm — and returns the first match. Two scopes holding one
// name is not an expected shape; this simply prefers the nearer.
func findSession(home string, roots []string, name string) (Session, error) {
	dirs := make([]string, len(roots))
	for i, root := range roots {
		dirs[i] = SessionDir(home, root)
	}
	return findSessionInDirs(dirs, name)
}

// findSessionInDirs is findSession's dir-addressed sibling, used by --all, which
// enumerates scope-key directories it cannot turn back into scope roots —
// ScopeKey is a one-way hash.
func findSessionInDirs(dirs []string, name string) (Session, error) {
	for _, dir := range dirs {
		metaPath := filepath.Join(dir, name+".meta.json")
		meta, err := LoadMeta(metaPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Session{}, err
		}
		sockPath := filepath.Join(dir, name+".sock")
		return Session{SocketPath: sockPath, MetaPath: metaPath, Meta: meta}, nil
	}
	return Session{}, notFoundError(name)
}

// readCode implements eval's code-source precedence: the positional argument
// wins; with none, code comes from stdin, but only when stdin is not a live
// terminal, so a bare `eval --name s` in an interactive shell fails loud instead
// of blocking on input nothing will supply. Neither present is ok=false.
func readCode(positional []string, stdin io.Reader) (code string, ok bool) {
	if len(positional) > 0 {
		return strings.Join(positional, " "), true
	}
	if isTerminalReader(stdin) {
		return "", false
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// isTerminalReader reports whether r is a live terminal — a read that would
// block rather than yield piped data. A nil reader answers true, like a real
// terminal: both mean nothing is there to read, which lets a test exercise the
// no-code path without a pty. Anything that is not an *os.File answers false,
// matching a genuinely piped os.Stdin.
func isTerminalReader(r io.Reader) bool {
	if r == nil {
		return true
	}
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return charmterm.IsTerminal(f.Fd())
}

// parseFlags parses args against fs and returns the positionals, allowing flags
// and positionals in any order — mirrors internal/bus/action.go's parseFlags,
// whose doc carries the full rationale. A bare "--" ends flag scanning, which is
// what lets `eval --name s -- '-1 + 2'` disambiguate dash-leading code.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			return positional, nil
		}

		name, looksLikeFlag := flagName(arg)
		if !looksLikeFlag {
			positional = append(positional, arg)
			i++
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			return nil, fs.Parse([]string{arg})
		}
		if strings.Contains(arg, "=") || isBoolFlag(f) {
			if err := fs.Parse([]string{arg}); err != nil {
				return nil, err
			}
			i++
			continue
		}
		if i+1 >= len(args) {
			return nil, fs.Parse([]string{arg})
		}
		if err := fs.Parse([]string{arg, args[i+1]}); err != nil {
			return nil, err
		}
		i += 2
	}
	return positional, nil
}

// flagName reports whether arg is shaped "--name" or "--name=value" and returns
// the bare name. Any other shape — including a positional starting with "-" —
// is ok=false, so parseFlags never risks it on fs.Parse.
func flagName(arg string) (name string, ok bool) {
	if !strings.HasPrefix(arg, "--") || len(arg) <= 2 {
		return "", false
	}
	name = arg[2:]
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		name = name[:eq]
	}
	if name == "" {
		return "", false
	}
	return name, true
}

// isBoolFlag reports whether f takes no argument, so parseFlags never swallows
// the next positional as a bool flag's value.
func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// startAction implements `atomic repl start`. spawn is nil in production;
// tests inject a stub so this runs without a real interpreter.
func startAction(args []string, home string, scopeRoots []string, spawn SpawnFunc, out io.Writer) int {
	const usage = "Usage: atomic repl start --name <s> --lang py|js [--env <file>] [--bin <path>] [--json]\n"

	fs := flag.NewFlagSet("repl-start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name, lang, envFile, bin string
	var jsonOut bool
	fs.StringVar(&name, "name", "", "session name")
	fs.StringVar(&lang, "lang", "", "py|js (aliases: python, node, javascript)")
	fs.StringVar(&envFile, "env", "", "KEY=VALUE file merged into the session's environment")
	fs.StringVar(&bin, "bin", "", "interpreter path override")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 0 {
		fmt.Fprintf(os.Stderr, "atomic repl start: unexpected argument(s): %v\n", positional)
		return int(ExitUsage)
	}
	if name == "" || lang == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	if err := ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl start: %v\n", err)
		return int(ExitUsage)
	}
	canonLang, err := resolveLang(lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl start: %v\n", err)
		return int(ExitUsage)
	}

	var envEntries []string
	if envFile != "" {
		envEntries, err = ParseEnvFile(envFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic repl start: %v\n", err)
			return int(ExitUsage)
		}
	}

	scopeRoot := scopeRoots[0]
	// Emitted before the spawn is attempted: the value is wrong either way, and a
	// start that fails for some other reason is exactly when a stale config line
	// is worth seeing.
	idleTimeout, idleWarnings := resolveIdleTimeout(home, scopeRoot)
	for _, w := range idleWarnings {
		fmt.Fprintf(os.Stderr, "atomic repl start: %s\n", w)
	}

	meta, alreadyRunning, err := EnsureStarted(StartOptions{
		Home:        home,
		ScopeRoot:   scopeRoot,
		Name:        name,
		Lang:        canonLang,
		Bin:         bin,
		Env:         envEntries,
		IdleTimeout: idleTimeout,
		Spawn:       spawn,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl start: %v\n", err)
		return int(exitCodeForErr(err))
	}

	if jsonOut {
		return emitJSON(out, startView{sessionView: metaToView(meta, true), AlreadyRunning: alreadyRunning})
	}
	verb := "started"
	if alreadyRunning {
		verb = "already running"
	}
	fmt.Fprintf(out, "%s: %s (pid %d)\n", verb, name, meta.PID)
	return int(ExitOK)
}

// evalAction implements `atomic repl eval`.
func evalAction(args []string, home string, scopeRoots []string, stdin io.Reader, out io.Writer) int {
	const usage = "Usage: atomic repl eval --name <s> [--timeout <duration>] [--json] [--] [<code>]\n"

	fs := flag.NewFlagSet("repl-eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name, timeoutStr string
	var jsonOut bool
	fs.StringVar(&name, "name", "", "session name")
	fs.StringVar(&timeoutStr, "timeout", "", "eval deadline (default 30s)")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if name == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	if err := ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl eval: %v\n", err)
		return int(ExitUsage)
	}

	var timeout time.Duration
	if timeoutStr != "" {
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic repl eval: --timeout: %v\n", err)
			return int(ExitUsage)
		}
	}

	code, ok := readCode(positional, stdin)
	if !ok {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	sess, err := findSession(home, scopeRoots, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl eval: %v\n", err)
		return int(exitCodeForErr(err))
	}

	resp, err := Eval(sess, code, EvalOptions{Timeout: timeout})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl eval: %v\n", dialError(err, name))
		return int(exitCodeForErr(err))
	}

	if jsonOut {
		if code := emitJSON(out, resp); code != int(ExitOK) {
			return code
		}
	} else {
		printEvalHuman(out, resp)
	}
	if !resp.OK {
		return int(ExitEvalException)
	}
	return int(ExitOK)
}

// printEvalHuman renders a Response for a terminal: stdout/stderr as produced, a
// "[truncated]" marker when either stream hit the cap, then the final value — or
// the traceback on stderr when the eval failed.
func printEvalHuman(out io.Writer, resp Response) {
	if resp.Stdout != "" {
		fmt.Fprint(out, resp.Stdout)
	}
	if resp.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Stderr)
	}
	if resp.Truncated {
		fmt.Fprintln(out, "[truncated]")
	}
	if !resp.OK {
		fmt.Fprintln(os.Stderr, resp.Error)
		return
	}
	if resp.Value != "" {
		fmt.Fprintln(out, resp.Value)
	}
}

// listAction implements `atomic repl list`. It always exits 0: a dead entry is
// reported inline, never as a command failure.
func listAction(args []string, home string, scopeRoots []string, out io.Writer) int {
	const usage = "Usage: atomic repl list [--all] [--json]\n"

	fs := flag.NewFlagSet("repl-list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var all, jsonOut bool
	fs.BoolVar(&all, "all", false, "enumerate every session on the machine, across every scope")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	var dirs []string
	if all {
		dirs, err = AllSessionDirs(home)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic repl list: %v\n", err)
			return int(ExitUsage)
		}
	} else {
		for _, root := range scopeRoots {
			dirs = append(dirs, SessionDir(home, root))
		}
	}

	views, err := listSessions(dirs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl list: %v\n", err)
		return int(ExitUsage)
	}

	if jsonOut {
		return emitJSON(out, views)
	}
	for _, v := range views {
		fmt.Fprintf(out, "%s\t%s\t%s\t%d\t%s\n", v.Name, v.Root, v.Lang, v.PID, livenessLabel(v.Alive))
	}
	return int(ExitOK)
}

// listSessions reads every session's meta under dirs and probes liveness
// directly, never failing the whole listing over one bad entry — which is what
// lets list stay a pure enumeration.
func listSessions(dirs []string) ([]sessionView, error) {
	var views []sessionView
	for _, dir := range dirs {
		names, err := sessionNamesInDir(dir)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			metaPath := filepath.Join(dir, name+".meta.json")
			meta, err := LoadMeta(metaPath)
			if err != nil {
				// Corrupt or mid-write meta: skip this entry, not the listing.
				continue
			}
			sockPath := filepath.Join(dir, name+".sock")
			views = append(views, metaToView(meta, IsLive(sockPath)))
		}
	}
	return views, nil
}

// sessionNamesInDir returns every session name with a meta file in dir, sorted.
// An absent dir means no session ever started there — empty, not an error.
func sessionNamesInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("repl: read session dir %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name, ok := strings.CutSuffix(e.Name(), ".meta.json"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// statusAction implements `atomic repl status`. --all broadens the search from
// the current repo/realm union to every scope on the machine, so a session can
// be found without knowing which repo or realm produced it.
func statusAction(args []string, home string, scopeRoots []string, out io.Writer) int {
	const usage = "Usage: atomic repl status --name <s> [--all] [--json]\n"

	fs := flag.NewFlagSet("repl-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name string
	var all, jsonOut bool
	fs.StringVar(&name, "name", "", "session name")
	fs.BoolVar(&all, "all", false, "search every scope on the machine, not just the current repo/realm")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 0 || name == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	if err := ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl status: %v\n", err)
		return int(ExitUsage)
	}

	var sess Session
	if all {
		dirs, dirErr := AllSessionDirs(home)
		if dirErr != nil {
			fmt.Fprintf(os.Stderr, "atomic repl status: %v\n", dirErr)
			return int(ExitUsage)
		}
		sess, err = findSessionInDirs(dirs, name)
	} else {
		sess, err = findSession(home, scopeRoots, name)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl status: %v\n", err)
		return int(exitCodeForErr(err))
	}

	client, err := Dial(sess.SocketPath, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl status: %v\n", dialError(err, name))
		return int(exitCodeForErr(err))
	}
	client.Close()

	view := metaToView(sess.Meta, true)
	if jsonOut {
		return emitJSON(out, view)
	}
	fmt.Fprintf(out, "%s\t%s\t%s\t%d\t%s\n", view.Name, view.Root, view.Lang, view.PID, livenessLabel(true))
	return int(ExitOK)
}

// resetAction implements `atomic repl reset`: clears the interpreter namespace
// without ending the harness process.
func resetAction(args []string, home string, scopeRoots []string, out io.Writer) int {
	const usage = "Usage: atomic repl reset --name <s> [--json]\n"

	fs := flag.NewFlagSet("repl-reset", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name string
	var jsonOut bool
	fs.StringVar(&name, "name", "", "session name")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 0 || name == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	if err := ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl reset: %v\n", err)
		return int(ExitUsage)
	}

	sess, err := findSession(home, scopeRoots, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl reset: %v\n", err)
		return int(exitCodeForErr(err))
	}

	client, err := Dial(sess.SocketPath, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl reset: %v\n", dialError(err, name))
		return int(exitCodeForErr(err))
	}
	defer client.Close()

	resp, err := client.Do(Request{V: ProtocolVersion, Op: OpReset})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl reset: %v\n", err)
		return int(exitCodeForErr(err))
	}

	if jsonOut {
		return emitJSON(out, resp)
	}
	fmt.Fprintf(out, "reset: %s\n", name)
	return int(ExitOK)
}

// stopAction implements `atomic repl stop`. The harness removes its own socket
// and meta on a clean shutdown ack; a session already dead when this runs is
// reported dead, like any other verb touching its socket.
func stopAction(args []string, home string, scopeRoots []string, out io.Writer) int {
	const usage = "Usage: atomic repl stop --name <s> [--json]\n"

	fs := flag.NewFlagSet("repl-stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var name string
	var jsonOut bool
	fs.StringVar(&name, "name", "", "session name")
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 0 || name == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	if err := ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl stop: %v\n", err)
		return int(ExitUsage)
	}

	sess, err := findSession(home, scopeRoots, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl stop: %v\n", err)
		return int(exitCodeForErr(err))
	}

	client, err := Dial(sess.SocketPath, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl stop: %v\n", dialError(err, name))
		return int(exitCodeForErr(err))
	}
	defer client.Close()

	resp, err := client.Do(Request{V: ProtocolVersion, Op: OpShutdown})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic repl stop: %v\n", err)
		return int(exitCodeForErr(err))
	}

	if jsonOut {
		return emitJSON(out, resp)
	}
	fmt.Fprintf(out, "stopped: %s\n", name)
	return int(ExitOK)
}
