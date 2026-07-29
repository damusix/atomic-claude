package bus

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	charmterm "github.com/charmbracelet/x/term"
)

// BusAction is the exported entry point for `atomic bus`, mirroring
// internal/wiki's WikiAction (internal/wiki/action.go:17): home is injected
// rather than resolved via os.UserHomeDir() internally, so
// cmd/atomic/main.go's runBus owns the one os.UserHomeDir() call and every
// path in this package stays testable against a temp dir. cwd is accepted
// for signature parity with WikiAction; nothing in bus needs it today (no
// --root-style flag), so it is unused.
func BusAction(args []string, home, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic bus <join|leave|send|recv|who|rooms|status|serve|start|stop|restart|tail|say|halt|resume|prune|chat> [flags]")
		return int(ExitUsage)
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "join":
		return joinAction(rest, home, out)
	case "leave":
		return leaveAction(rest, home, out)
	case "send":
		return sendAction(rest, home, out)
	case "recv":
		return recvAction(rest, home, out)
	case "who":
		return whoAction(rest, home, out)
	case "rooms":
		return roomsAction(rest, home, out)
	case "status":
		return statusAction(rest, home, out)
	case "serve":
		return serveAction(rest, home, out)
	case "start":
		return startAction(rest, home, out)
	case "stop":
		return stopAction(rest, home, out)
	case "restart":
		return restartAction(rest, home, out)
	case "tail":
		return tailAction(rest, home, out)
	case "say":
		return sayAction(rest, home, out)
	case "halt":
		return haltAction(rest, home, out)
	case "resume":
		return resumeAction(rest, home, out)
	case "prune":
		return pruneAction(rest, home, out)
	case "chat":
		return chatAction(rest, home, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic bus: unknown verb %q\n", verb)
		return int(ExitUsage)
	}
}

// parseFlags parses args against fs and returns the positional arguments,
// supporting flags and positionals in any order. Every verb in this package
// documents its positional argument(s) before its flags (e.g. "join <room>
// --as <name>"), but flag.FlagSet.Parse stops at the first non-flag token
// and leaves everything from there on — including any later flags — in
// Args(), so a single fs.Parse(args) call would silently leave --as
// unparsed.
//
// This scans args itself, one token at a time, and classifies a token as a
// flag only when it has the shape "--name" or "--name=value" AND name is
// registered on fs (flag.FlagSet.Lookup) — every flag this package defines
// is long-form ("--as", "--mode", "--json", ...), so that is the complete
// flag grammar here. Anything else — a bare positional, a single-dash
// token, the literal "-" stdin sentinel, or a positional that happens to
// start with "-" (a diff line, a negative number, a quoted flag) — is
// never handed to fs.Parse and so can never trip its "flag provided but
// not defined" rejection. A bare "--" terminates flag scanning; every
// remaining token, however it's shaped, is positional. A "--name" that
// matches no registered flag is still a hard usage error (delegated to
// fs.Parse for its standard message) — only the positional case is
// rescued, not unknown flags.
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
			// Delegate to fs.Parse for the standard "flag provided but not
			// defined" error and exit behavior.
			return nil, fs.Parse([]string{arg})
		}
		if strings.Contains(arg, "=") || isBoolFlag(f) {
			// "--name=value" is self-contained; a bool flag takes no
			// separate value token (mirrors flag.FlagSet's own grammar).
			if err := fs.Parse([]string{arg}); err != nil {
				return nil, err
			}
			i++
			continue
		}
		if i+1 >= len(args) {
			// No value token left: let fs.Parse produce "flag needs an
			// argument".
			return nil, fs.Parse([]string{arg})
		}
		// The next token is this flag's value verbatim, whatever it looks
		// like — including one that itself starts with "-".
		if err := fs.Parse([]string{arg, args[i+1]}); err != nil {
			return nil, err
		}
		i += 2
	}
	return positional, nil
}

// flagName reports whether arg has the shape "--name" or "--name=value" —
// the only flag shape this package registers — and if so, name with the
// leading "--" and any "=value" suffix stripped. Any other shape (a bare
// "-", a single-dash token, "--" alone) returns ok=false so the caller
// treats arg as positional instead of risking it on fs.Parse.
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

// isBoolFlag reports whether f takes no argument, using the same
// interface flag.FlagSet checks internally — a bool flag's Value
// implements IsBoolFlag() bool. Without this, "--json" or "--all-rooms"
// would swallow the next positional as their value.
func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// exitFromErr maps err to the process exit code the CLI should terminate
// with: a *bus.Error's own Code when err carries one (the daemon assigned
// it, or a client-side check like SessionID/ResolveRoom did — see
// protocol.go's Response.Code doc), else ExitHard. Every action below routes
// through this instead of re-deriving a code from error text.
func exitFromErr(err error) int {
	var busErr *Error
	if errors.As(err, &busErr) {
		return int(busErr.Code)
	}
	return int(ExitHard)
}

// dialDaemon connects to the daemon without spawning it. Every verb except
// join assumes join already spawned one — spawning here too would mean a
// stray `atomic bus who` on a cold machine silently brings up a daemon just
// to answer a query. A dial failure means no daemon is listening, which
// exit code 6 (ExitUnreachable) says plainly; Dial's own error is a plain
// wrapped error, not a *bus.Error, so it is remapped here rather than
// falling through exitFromErr's generic ExitHard default.
func dialDaemon(home string) (*Client, error) {
	client, err := Dial(home, defaultDialTimeout)
	if err != nil {
		return nil, &Error{Code: ExitUnreachable, Msg: fmt.Sprintf("bus: daemon unreachable: %v", err)}
	}
	return client, nil
}

// recoveryEnsurer is a package-level testable seam
// (.claude/skills/atomic-cli-contrib/SKILL.md §2): production callers get
// DefaultEnsurer(); tests substitute an Ensurer whose Spawn starts an
// in-process daemon (client_test.go's own pattern for EnsureDaemon) instead
// of depending on a real `atomic` binary on PATH.
var recoveryEnsurer = DefaultEnsurer

// dialDaemonRecovered dials the daemon, and — only when it is unreachable —
// respawns it via EnsureDaemon and retries exactly once
// (docs/spec/atomic-bus.md: "a client that finds the daemon gone respawns
// it and retries once before surfacing exit 6"). A stop or a crash both
// tear the daemon process down along with its in-memory roster, but
// bus.json already holds every session's membership, and the respawned
// daemon's own Hub.Rehydrate call at Serve startup (see serveAction)
// restores that whole roster before it accepts a single connection — so
// recovery here is nothing more than getting a live daemon back; there is
// no client-side rejoin left to do (see docs/spec/atomic-bus.md's "the
// daemon rehydrates the roster" change-log entry, which replaced the
// per-session re-registration this used to do). EnsureDaemon owns its own
// bounded spawn-and-retry loop, so a daemon that still won't come back
// surfaces that terminal error directly — never a second recovery attempt
// on top of it.
func dialDaemonRecovered(home string) (*Client, error) {
	client, err := dialDaemon(home)
	if err == nil {
		return client, nil
	}
	var busErr *Error
	if !errors.As(err, &busErr) || busErr.Code != ExitUnreachable {
		return nil, err
	}
	return recoveryEnsurer().EnsureDaemon(home)
}

// doWithRecovery performs req against a daemon guaranteed live by
// dialDaemonRecovered above. Used only by send and leave, the two ops that
// require this session to already be a member of req.Room. An ExitNotJoined
// response is passed through untouched, never treated as a recovery
// symptom: a restarted daemon already knows every persisted member the
// instant it starts serving (Hub.Rehydrate), so ExitNotJoined here means
// exactly what it says — this session genuinely never joined, or
// explicitly left, the room — and masking that would be worse than
// surfacing it plainly.
func doWithRecovery(home string, req Request) (Response, error) {
	client, err := dialDaemonRecovered(home)
	if err != nil {
		return Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// joinAction implements `atomic bus join <room> --as <name> [--mode
// participate|observe] [--kind agent|human] [--session <id>]`. The
// numeric-suffix retry on a name collision is Hub.Join's job (room.go) —
// this only reports the assigned name, which may differ from the one
// requested. --kind defaults to agent; a person joining from a terminal
// passes --kind human so from_kind-keyed reaction-policy rules fire for
// them (docs/spec/atomic-bus.md: "join --kind agent|human ... lets a person
// joining from a terminal be recorded as human"). chat still hardcodes
// KindHuman on its own OpJoin call below — it has no reason to ever join as
// an agent.
func joinAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus join <room> --as <name> [--mode participate|observe] [--kind agent|human] [--session <id>]\n"

	fs := flag.NewFlagSet("bus-join", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var as, mode, kind, session string
	fs.StringVar(&as, "as", "", "member name to claim in the room (required)")
	fs.StringVar(&mode, "mode", "participate", "participate or observe")
	fs.StringVar(&kind, "kind", KindAgent, "agent or human")
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 1 || as == "" {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room := positional[0]

	if mode != "participate" && mode != "observe" {
		fmt.Fprintf(os.Stderr, "atomic bus join: --mode must be participate or observe, got %q\n", mode)
		return int(ExitUsage)
	}
	if !validKind(kind) {
		fmt.Fprintf(os.Stderr, "atomic bus join: --kind must be %q or %q, got %q\n", KindAgent, KindHuman, kind)
		return int(ExitUsage)
	}

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return exitFromErr(err)
	}

	// Through the recoveryEnsurer seam, not the package-level EnsureDaemon:
	// the bare call bypasses the injection point, so tests exercising join
	// reach the real spawnServe. Under `go test` that re-execs the test
	// binary, which re-runs the suite, which calls join again — a fork bomb.
	client, err := recoveryEnsurer().EnsureDaemon(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpJoin, Room: room, Name: as, Mode: mode, Kind: kind, Session: sessionID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return exitFromErr(err)
	}

	var payload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: parse response: %v\n", err)
		return int(ExitHard)
	}

	st, err := Load(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return int(ExitHard)
	}
	st.Join(sessionID, room, payload.Name, mode, kind)
	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return int(ExitHard)
	}

	if payload.Name != as {
		fmt.Fprintf(out, "joined %s as %s (requested %s was taken)\n", room, payload.Name, as)
	} else {
		fmt.Fprintf(out, "joined %s as %s\n", room, payload.Name)
	}
	return int(ExitOK)
}

// leaveAction implements `atomic bus leave [<room>] [--session <id>]`. A
// missing room defaults to the session's last-joined room via
// State.ResolveRoom; leaving clears local state for that room only, per the
// brief.
func leaveAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus leave [<room>] [--session <id>]\n"

	fs := flag.NewFlagSet("bus-leave", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var session string
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	var explicit string
	if len(positional) == 1 {
		explicit = positional[0]
	}

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return exitFromErr(err)
	}

	st, err := Load(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return int(ExitHard)
	}

	room, err := st.ResolveRoom(sessionID, explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return exitFromErr(err)
	}

	if _, err := doWithRecovery(home, Request{Op: OpLeave, Room: room, Session: sessionID}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return exitFromErr(err)
	}

	st.Leave(sessionID, room)
	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return int(ExitHard)
	}

	fmt.Fprintf(out, "left %s\n", room)
	return int(ExitOK)
}

// sendAction implements `atomic bus send <room> <text> [--to
// <name>,...] [--reply-to <msg-id>] [--session <id>]`.
func sendAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus send <room> <text> [--to <name>,...] [--reply-to <msg-id>] [--session <id>] [--json]\n"

	fs := flag.NewFlagSet("bus-send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var to, replyTo, session string
	var jsonOut bool
	fs.StringVar(&to, "to", "", "comma-separated addressee names (omitted means FYI to the whole room)")
	fs.StringVar(&replyTo, "reply-to", "", "id of the message being replied to")
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	fs.BoolVar(&jsonOut, "json", false, "emit the full envelope as JSON (captures the id for --reply-to)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 2 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room, textArg := positional[0], positional[1]

	text, err := readText(textArg, os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus send: %v\n", err)
		return int(ExitHard)
	}

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus send: %v\n", err)
		return exitFromErr(err)
	}

	resp, err := doWithRecovery(home, Request{Op: OpSend, Room: room, Session: sessionID, To: parseTo(to), ReplyTo: replyTo, Text: text})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus send: %v\n", err)
		return exitFromErr(err)
	}

	var payload struct {
		Envelope  Envelope `json:"envelope"`
		UnknownTo []string `json:"unknown_to,omitempty"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus send: parse response: %v\n", err)
		return int(ExitHard)
	}

	// The message is still delivered and this still exits 0 — a named
	// addressee may legitimately be about to join (docs/spec/atomic-bus.md:
	// "send --to <name> warns on stderr when no such member is in the
	// room") — but the sender must not be told nothing.
	if len(payload.UnknownTo) > 0 {
		fmt.Fprintf(os.Stderr, "atomic bus send: warning: not currently in room %s: %s\n", room, strings.Join(payload.UnknownTo, ", "))
	}

	if jsonOut {
		return emitJSON(out, payload.Envelope)
	}
	// A bare id ("1") on success is noise for a human and under-structured
	// for an agent capturing it — --json above is the structured path; this
	// is a short confirmation naming the id for reference, not the id alone.
	fmt.Fprintf(out, "sent to %s (id %s)\n", room, payload.Envelope.ID)
	return int(ExitOK)
}

// readText returns text verbatim, or the full content of stdin — read
// whole, never line-scanned — when text is "-". This is the path an agent
// uses to send a multi-line payload (e.g. a stack trace) without it passing
// through shell quoting.
func readText(text string, stdin io.Reader) (string, error) {
	if text != "-" {
		return text, nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("bus: read stdin: %w", err)
	}
	return string(b), nil
}

// parseTo splits a comma-separated --to value into addressee names. An
// empty string (the flag omitted) yields nil, not [""] — nil is what marks
// an envelope as an FYI to the whole room rather than addressed to one
// blank name (see protocol.go's Envelope.To doc: omitted --to means FYI,
// addressed to nobody).
func parseTo(to string) []string {
	if to == "" {
		return nil
	}
	parts := strings.Split(to, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// recvAction implements `atomic bus recv <room> [--session <id>] [--json]`.
// recv always streams: one JSON envelope per line, flushed per line,
// exiting 0 on SIGTERM — there is no one-shot mode and no --follow flag to
// forget (a `recv` that returned and exited would leave a Monitor silently
// hearing nothing; see docs/spec/atomic-bus.md's "replay removed entirely"
// change-log entry). --json is accepted for consistency with every other
// read verb but is a no-op: the stream is already one JSON envelope per
// line.
//
// recv now resolves its own session identity (previously it needed none) so
// the daemon can suppress this subscriber's own publishes (SkipSelf on the
// subscribe request) — self-echo would otherwise cost this agent one wasted
// prompt per message it sends (docs/spec/atomic-bus.md: "a subscriber does
// not receive its own published messages"). A recv with no resolvable
// session (no CLAUDE_CODE_SESSION_ID, no --session) fails exactly like
// send/leave/join do, rather than silently degrading self-echo suppression
// — every real recv already runs inside a live Claude Code session, so this
// is not a new practical restriction.
func recvAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus recv <room> [--session <id>] [--json]\n"

	fs := flag.NewFlagSet("bus-recv", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	var session string
	fs.BoolVar(&jsonOut, "json", false, "no-op: recv always streams one JSON envelope per line")
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room := positional[0]

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}
	return recvStream(client, room, sessionID, out)
}

// recvStream is the Monitor path: one JSON envelope per line, flushed per
// line — json.Encoder.Encode issues exactly one Write per call, and out is
// the raw stdout stream with nothing buffering in front of it, so every
// line reaches the reader the instant it's written. Termination is checked
// only at loop-iteration boundaries (the select below), never mid-write, so
// a SIGTERM/SIGINT arriving while a line is being written cannot truncate
// it — the current write always completes before the next select decides
// whether to exit. A buffered or dropped line here is the entire recv
// feature failing silently (docs/spec/atomic-bus.md). No backlog is
// replayed: this subscribes and delivers only what is published after —
// see daemon.go's subscribe doc. SkipSelf is always set: this session's own
// sends are exactly what a recv subscriber should never see back
// (self-echo — docs/spec/atomic-bus.md).
func recvStream(client *Client, room, sessionID string, out io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ch, err := client.Subscribe(Request{Op: OpRecv, Room: room, Session: sessionID, SkipSelf: true})
	if err != nil {
		client.Close()
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	enc := json.NewEncoder(out)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return int(ExitOK)
			}
			if err := enc.Encode(env); err != nil {
				fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
				return int(ExitHard)
			}
		case <-ctx.Done():
			return int(ExitOK)
		}
	}
}

// whoAction implements `atomic bus who [<room>] [--json]`. A room named
// explicitly needs no session identity at all — only the fallback to the
// session's last-joined room does — so an operator can inspect any room by
// name outside a live Claude Code session.
func whoAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus who [<room>] [--json]\n"

	fs := flag.NewFlagSet("bus-who", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	var explicit string
	if len(positional) == 1 {
		explicit = positional[0]
	}

	room, err := resolveOptionalRoom(home, explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus who: %v\n", err)
		return exitFromErr(err)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus who: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpWho, Room: room})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus who: %v\n", err)
		return exitFromErr(err)
	}
	var payload struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus who: parse response: %v\n", err)
		return int(ExitHard)
	}

	if jsonOut {
		return emitJSON(out, payload.Members)
	}
	for _, m := range payload.Members {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", m.Name, m.Kind, m.Mode, livenessLabel(m.Stale))
	}
	return int(ExitOK)
}

// livenessLabel renders Member.Stale for plain-text output — shared by
// whoAction and render.go's MemberTable so `who` and chat's `/who` agree on
// the same two words.
func livenessLabel(stale bool) string {
	if stale {
		return "stale"
	}
	return "live"
}

// resolveOptionalRoom returns explicit verbatim, or — only when explicit is
// empty — resolves the current session's last-joined room via
// State.ResolveRoom. Session identity is only needed in that fallback
// branch, so a caller that names a room outright never has to be inside a
// live Claude Code session.
func resolveOptionalRoom(home, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	sessionID, err := SessionID("")
	if err != nil {
		return "", err
	}
	st, err := Load(home)
	if err != nil {
		return "", fmt.Errorf("bus: %w", err)
	}
	return st.ResolveRoom(sessionID, "")
}

// roomsAction implements `atomic bus rooms [--json]`.
func roomsAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus rooms [--json]\n"

	fs := flag.NewFlagSet("bus-rooms", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus rooms: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpRooms})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus rooms: %v\n", err)
		return exitFromErr(err)
	}
	var payload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus rooms: parse response: %v\n", err)
		return int(ExitHard)
	}

	if jsonOut {
		return emitJSON(out, payload.Rooms)
	}
	for _, r := range payload.Rooms {
		fmt.Fprintf(out, "%s\t%d\n", r.Name, r.Members)
	}
	return int(ExitOK)
}

// busStatus is the JSON shape `atomic bus status --json` emits, and the
// data the plain-text path renders from.
type busStatus struct {
	Session string             `json:"session"`
	Daemon  string             `json:"daemon"` // "running", "not running", or "unreachable"
	Version int                `json:"version,omitempty"`
	Pid     int                `json:"pid,omitempty"`
	Rooms   []joinedRoomStatus `json:"rooms"`
}

// joinedRoomStatus is one entry of busStatus.Rooms: the room and the name
// this session holds in it (a join may have been renamed by the
// numeric-suffix retry).
type joinedRoomStatus struct {
	Room string `json:"room"`
	Name string `json:"name"`
}

// statusAction implements `atomic bus status [--session <id>] [--json]`:
// this session's joined rooms (from local state) plus the daemon's own
// reachability and identity. Unlike join, status never spawns a daemon —
// an unreachable daemon is exactly what status is for reporting, not a
// condition to fix.
func statusAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus status [--session <id>] [--json]\n"

	fs := flag.NewFlagSet("bus-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	var session string
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus status: %v\n", err)
		return exitFromErr(err)
	}

	st, err := Load(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus status: %v\n", err)
		return int(ExitHard)
	}

	status := busStatus{Session: sessionID, Daemon: "not running", Rooms: joinedRooms(st, sessionID)}

	if client, derr := dialDaemon(home); derr == nil {
		defer client.Close()
		if resp, perr := client.Do(Request{Op: OpPing}); perr == nil {
			var payload struct {
				Version int `json:"version"`
				Pid     int `json:"pid"`
			}
			if json.Unmarshal(resp.Payload, &payload) == nil {
				status.Daemon = "running"
				status.Version = payload.Version
				status.Pid = payload.Pid
			}
		} else {
			status.Daemon = "unreachable"
		}
	}

	if jsonOut {
		return emitJSON(out, status)
	}

	if status.Daemon == "running" {
		fmt.Fprintf(out, "daemon: running (v%d, pid %d)\n", status.Version, status.Pid)
	} else {
		fmt.Fprintf(out, "daemon: %s\n", status.Daemon)
	}
	if len(status.Rooms) == 0 {
		fmt.Fprintln(out, "not joined any room")
	} else {
		for _, r := range status.Rooms {
			fmt.Fprintf(out, "%s\t%s\n", r.Room, r.Name)
		}
	}
	return int(ExitOK)
}

// joinedRooms returns session's joined rooms sorted by room name — st's
// membership map has no stable iteration order, and status output must.
func joinedRooms(st *State, session string) []joinedRoomStatus {
	ss, ok := st.Sessions[session]
	if !ok {
		return nil
	}
	out := make([]joinedRoomStatus, 0, len(ss.Rooms))
	for room, m := range ss.Rooms {
		out = append(out, joinedRoomStatus{Room: room, Name: m.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Room < out[j].Room })
	return out
}

// emitJSON writes v as a single JSON value followed by a newline. Every
// --json read verb that answers with a snapshot (who, rooms, status) uses
// this; recv always streams JSONL instead, one envelope per line — see
// recvStream.
func emitJSON(out io.Writer, v any) int {
	if err := json.NewEncoder(out).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus: encode JSON: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// serveAction implements `atomic bus serve`: runs the daemon in the
// foreground — this is exactly what EnsureDaemon spawns (client.go's
// spawnServe) — binding the real socket, owning the socket file and this
// invocation's share of the spawn-lock protocol (Serve itself only owns
// what happens once it has a live listener; see daemon.go's Serve doc).
// There is no --idle-shutdown-minutes flag and no --stop flag: no timer
// ever retires the daemon on its own, and stopping one is `atomic bus
// stop`'s job (docs/spec/atomic-bus.md: "atomic bus start | stop | restart
// control the daemon explicitly").
func serveAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus serve\n"

	fs := flag.NewFlagSet("bus-serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	if err := EnsureDirs(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus serve: %v\n", err)
		return int(ExitHard)
	}

	ln, err := net.Listen("unix", SocketPath(home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus serve: listen: %v\n", err)
		return int(ExitUnreachable)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := NewHub(home)
	// A restarted daemon must come back with the whole persisted roster,
	// not rebuild it one session at a time as each happens to run a
	// command (docs/spec/atomic-bus.md: "a restarted daemon rehydrates the
	// whole roster"). This runs before ln.Accept ever sees a connection —
	// Serve itself does not start its accept loop until it's called below.
	// A missing bus.json is not an error (Load's own contract); a
	// malformed one degrades to an empty roster with a warning rather than
	// stopping the daemon from coming up at all.
	if st, err := Load(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus serve: warning: could not load %s, starting with an empty roster: %v\n", StatePath(home), err)
	} else {
		hub.Rehydrate(st)
	}

	// nil == ok: Serve returns nil on a wire shutdown (`bus stop`), and
	// context.Canceled on our own signal-driven ctx — both are a clean
	// stop, not a failure to report.
	if err := Serve(ctx, ln, hub, nil); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "atomic bus serve: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// startAction implements `atomic bus start`: spawns the daemon if none is
// listening. Idempotent — a daemon already live and version-matched is
// reported as such and left alone, rather than spawned again — and goes
// through the same recoveryEnsurer seam every other verb's recovery path
// uses (see joinAction's comment), so there is exactly one spawn
// implementation, not two.
func startAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus start\n"

	fs := flag.NewFlagSet("bus-start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	alreadyRunning := probeRunning(home)

	client, err := recoveryEnsurer().EnsureDaemon(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus start: %v\n", err)
		return exitFromErr(err)
	}
	client.Close()

	if alreadyRunning {
		fmt.Fprintln(out, "daemon already running")
	} else {
		fmt.Fprintln(out, "daemon started")
	}
	return int(ExitOK)
}

// probeRunning reports whether a live, version-matched daemon is already
// listening — used only to choose start's message. EnsureDaemon's own
// flock (client.go) is what actually makes concurrent start calls
// idempotent, not this probe: two racing starts can both observe "not
// running" here and both print "daemon started", but only one of them
// spawns — the lock, not the message, is the correctness guarantee.
func probeRunning(home string) bool {
	client, err := dialDaemon(home)
	if err != nil {
		return false
	}
	defer client.Close()
	return checkVersion(client) == nil
}

// stopAction implements `atomic bus stop`: sends the wire shutdown op to a
// running daemon. No daemon running is treated as already-stopped (exit 0,
// not an error): stop's job is to reach the goal state "no daemon", which a
// missing daemon has already reached — this is also the documented remedy
// the version-skew error message points users at (via restartAction), so
// it must succeed even when there is nothing to stop.
func stopAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus stop\n"

	fs := flag.NewFlagSet("bus-stop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	client, err := Dial(home, defaultDialTimeout)
	if err != nil {
		fmt.Fprintln(out, "no daemon running")
		return int(ExitOK)
	}
	defer client.Close()

	if _, err := client.Do(Request{Op: OpShutdown}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus stop: %v\n", err)
		return exitFromErr(err)
	}
	fmt.Fprintln(out, "daemon stopped")
	return int(ExitOK)
}

// restartWaitTimeout bounds restartAction's wait for stop's socket teardown
// to complete before start tries to dial or bind it again. Serve's actual
// listener Close() (and the unlink it performs) runs asynchronously in its
// own run-loop goroutine, after the shutdown op's reply is already sent
// (daemon.go's Serve doc) — so a start immediately following stop can race
// a socket file that hasn't been removed yet.
const restartWaitTimeout = 2 * time.Second

// restartAction implements `atomic bus restart`: stop then start. Works
// whether or not a daemon is currently running — stopAction's own
// "no daemon" case is exit 0, so restart degenerates cleanly to a plain
// start — and is what the version-skew error tells a user to run.
func restartAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus restart\n"

	fs := flag.NewFlagSet("bus-restart", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	if code := stopAction(nil, home, out); code != int(ExitOK) {
		return code
	}
	waitForSocketGone(home, restartWaitTimeout)

	return startAction(nil, home, out)
}

// waitForSocketGone polls SocketPath(home) until it refuses connections or
// timeout elapses. Best-effort: even a timeout here still lets
// startAction's own EnsureDaemon proceed — its stale-socket recovery
// (client.go: unlinkStaleSocket, one respawn retry) reaches a live daemon
// regardless — this just avoids relying on that retry for the common case.
func waitForSocketGone(home string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", SocketPath(home), 50*time.Millisecond)
		if err != nil {
			return
		}
		conn.Close()
		time.Sleep(10 * time.Millisecond)
	}
}

// --- halt / resume ---

// haltAction implements `atomic bus halt <room> [--text <reason>]`. Halt is
// enforced server-side (room.go's Hub.Publish) — this only sends the wire
// op; needs no session identity, since an operator can halt a room whether
// or not they are currently in it (room.go's Hub.Halt doc).
func haltAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus halt <room> [--text <reason>]\n"

	fs := flag.NewFlagSet("bus-halt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var text string
	fs.StringVar(&text, "text", "", "reason broadcast with the halt")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room := positional[0]

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus halt: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	if _, err := client.Do(Request{Op: OpHalt, Room: room, Text: text}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus halt: %v\n", err)
		return exitFromErr(err)
	}

	fmt.Fprintf(out, "halted %s\n", room)
	return int(ExitOK)
}

// resumeAction implements `atomic bus resume <room>`.
func resumeAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus resume <room>\n"

	fs := flag.NewFlagSet("bus-resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() != 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room := fs.Arg(0)

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus resume: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	if _, err := client.Do(Request{Op: OpResume, Room: room}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus resume: %v\n", err)
		return exitFromErr(err)
	}

	fmt.Fprintf(out, "resumed %s\n", room)
	return int(ExitOK)
}

// --- prune ---

// pruneAction implements `atomic bus prune [<room>] [--json]`. Removes only
// members Hub.Prune finds currently stale — never a live one, and never on
// its own: this is the one explicit reap the package performs, run only
// when an operator asks for it (docs/spec/atomic-bus.md: "nothing reaps a
// member silently ... a quiet session is not a dead one, and evicting a
// live member would break addressing with no diagnostic"). A missing room
// defaults to the session's last-joined room via State.ResolveRoom, same as
// who.
func pruneAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus prune [<room>] [--json]\n"

	fs := flag.NewFlagSet("bus-prune", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	var explicit string
	if len(positional) == 1 {
		explicit = positional[0]
	}

	room, err := resolveOptionalRoom(home, explicit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus prune: %v\n", err)
		return exitFromErr(err)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus prune: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpPrune, Room: room})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus prune: %v\n", err)
		return exitFromErr(err)
	}
	var payload struct {
		Removed []string `json:"removed"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus prune: parse response: %v\n", err)
		return int(ExitHard)
	}

	if jsonOut {
		return emitJSON(out, payload)
	}
	if len(payload.Removed) == 0 {
		fmt.Fprintf(out, "nothing to prune in %s\n", room)
	} else {
		fmt.Fprintf(out, "pruned %s from %s\n", strings.Join(payload.Removed, ", "), room)
	}
	return int(ExitOK)
}

// --- say ---

// sayAction implements `atomic bus say <room> <text> [--to <name>,...]`.
// Publishes via OpSay (Hub.PublishAsOperator), which needs no prior join and always
// passes, even into a halted room — the asymmetry that makes halt useful
// for an operator (docs/spec/atomic-bus.md: "say is a human send and
// always passes, even into a halted room").
func sayAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus say <room> <text> [--to <name>,...]\n"

	fs := flag.NewFlagSet("bus-say", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var to string
	fs.StringVar(&to, "to", "", "comma-separated addressee names (omitted means FYI to the whole room)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 2 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room, textArg := positional[0], positional[1]

	text, err := readText(textArg, os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus say: %v\n", err)
		return int(ExitHard)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus say: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	// No Name or Kind: the daemon pins the operator identity itself and ignores
	// both fields on OpSay. Sending them would imply the client gets a say in
	// who it publishes as, which is exactly the trust the daemon must not extend.
	resp, err := client.Do(Request{Op: OpSay, Room: room, To: parseTo(to), Text: text})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus say: %v\n", err)
		return exitFromErr(err)
	}

	var payload struct {
		Envelope  Envelope `json:"envelope"`
		UnknownTo []string `json:"unknown_to,omitempty"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus say: parse response: %v\n", err)
		return int(ExitHard)
	}

	// Mirrors sendAction's own warning-not-withholding contract
	// (docs/spec/atomic-bus.md: "send --to <name> warns on stderr when no
	// such member is in the room").
	if len(payload.UnknownTo) > 0 {
		fmt.Fprintf(os.Stderr, "atomic bus say: warning: not currently in room %s: %s\n", room, strings.Join(payload.UnknownTo, ", "))
	}

	fmt.Fprintf(out, "said to %s (id %s)\n", room, payload.Envelope.ID)
	return int(ExitOK)
}

// --- tail ---

// isTerminalWriter reports whether out is a live terminal — TailLine's
// colour switch (docs/spec/atomic-bus.md CP5: "detect no-tty and drop
// colour"). Anything that isn't a *os.File (a bytes.Buffer in tests, or a
// pipe wrapped by something other than os.File) is treated as non-tty,
// which is also the correct answer for a redirected or piped os.Stdout.
func isTerminalWriter(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	return charmterm.IsTerminal(f.Fd())
}

// terminalWidth reports out's terminal column width, or defaultLineWidth
// (render.go) when out is not a live terminal or its size can't be read.
func terminalWidth(out io.Writer) int {
	f, ok := out.(*os.File)
	if !ok {
		return defaultLineWidth
	}
	w, _, err := charmterm.GetSize(f.Fd())
	if err != nil || w <= 0 {
		return defaultLineWidth
	}
	return w
}

// resolveTailRooms decides which rooms tail subscribes to, and whether
// each rendered line needs a room prefix. An explicit room subscribes to
// exactly that room, unprefixed. Otherwise every room the daemon currently
// knows about is queried via `rooms`: docs/spec/atomic-bus.md CP5, quoted
// verbatim, "[--all-rooms] ... is the default when no room argument is
// given and exactly one room exists" — so a bare `tail` with exactly one
// known room, or --all-rooms given explicitly, subscribes to every room
// found (with a room prefix). Any other room count with neither an
// explicit room nor --all-rooms is ambiguous and is refused rather than
// silently guessed.
func resolveTailRooms(home, explicit string, allRoomsFlag bool) (rooms []string, roomPrefix bool, err error) {
	if explicit != "" {
		return []string{explicit}, false, nil
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		return nil, false, err
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpRooms})
	if err != nil {
		return nil, false, err
	}
	var payload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return nil, false, fmt.Errorf("bus: parse rooms response: %w", err)
	}
	names := make([]string, len(payload.Rooms))
	for i, r := range payload.Rooms {
		names[i] = r.Name
	}

	if !allRoomsFlag && len(names) != 1 {
		return nil, false, &Error{Code: ExitUsage, Msg: "bus: tail needs a room, or --all-rooms to watch every room"}
	}
	return names, true, nil
}

// tailAction implements `atomic bus tail [<room>] [--all-rooms] [--json]
// [--only-addressed] [--from <name>]`. tail never joins (resolveTailRooms
// and daemon.go's OpTail dispatch both operate purely through
// Hub.Subscribe) — it does not occupy a name and does not appear in `who`,
// so any number of operators can watch the same room at once. Like recv,
// tail delivers only what is published after it subscribes; there is no
// --since.
func tailAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus tail [<room>] [--all-rooms] [--json] [--only-addressed] [--from <name>]\n"

	fs := flag.NewFlagSet("bus-tail", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var allRoomsFlag, jsonOut, onlyAddressed bool
	var from string
	fs.BoolVar(&allRoomsFlag, "all-rooms", false, "interleave every room, prefixed per line")
	fs.BoolVar(&jsonOut, "json", false, "emit JSONL instead of rendered lines")
	fs.BoolVar(&onlyAddressed, "only-addressed", false, "show only messages with an explicit addressee")
	fs.StringVar(&from, "from", "", "show only messages from this sender")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	var explicitRoom string
	if len(positional) == 1 {
		explicitRoom = positional[0]
	}
	if explicitRoom != "" && allRoomsFlag {
		fmt.Fprintln(os.Stderr, "atomic bus tail: --all-rooms cannot be combined with an explicit room")
		return int(ExitUsage)
	}

	rooms, roomPrefix, err := resolveTailRooms(home, explicitRoom, allRoomsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus tail: %v\n", err)
		return exitFromErr(err)
	}

	client, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus tail: %v\n", err)
		return exitFromErr(err)
	}

	colour := isTerminalWriter(out)
	width := terminalWidth(out)
	return tailStream(client, rooms, onlyAddressed, from, jsonOut, colour, roomPrefix, home, width, out)
}

// tailStream is tailAction's subscription loop, factored out so tests can
// drive it against an already-connected *Client — mirrors recvStream's own
// factoring above, for the identical reason: closing the client is the
// only clean way to end a subscription loop deterministically in a test.
// Filtering (--only-addressed, --from) happens here, client-side: the
// daemon's OpTail dispatch (daemon.go) delivers every envelope on the
// subscribed rooms unfiltered by design, so two operators tailing the same
// room with different filters never affect each other.
func tailStream(client *Client, rooms []string, onlyAddressed bool, from string, jsonOut, colour, roomPrefix bool, home string, width int, out io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ch, err := client.Subscribe(Request{Op: OpTail, Rooms: rooms})
	if err != nil {
		client.Close()
		fmt.Fprintf(os.Stderr, "atomic bus tail: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	enc := json.NewEncoder(out)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return int(ExitOK)
			}
			if onlyAddressed && len(env.To) == 0 {
				continue
			}
			if from != "" && env.From != from {
				continue
			}
			if jsonOut {
				if err := enc.Encode(env); err != nil {
					fmt.Fprintf(os.Stderr, "atomic bus tail: %v\n", err)
					return int(ExitHard)
				}
				continue
			}
			fmt.Fprintln(out, TailLine(env, home, width, colour, roomPrefix))
		case <-ctx.Done():
			return int(ExitOK)
		}
	}
}

// --- chat ---

// chatAction implements `atomic bus chat <room> [--as <name>] [--session
// <id>]`: an interactive client that joins room as a kind: "human" member
// (docs/spec/atomic-bus.md checkpoint 6) and then hands off to Chat's core
// loop (chat.go) against a real raw-mode stdin and the daemon's live
// subscription stream. --as defaults to $USER; identity is resolved
// exactly like join (SessionID, --session override) — chat calls Hub.Join
// too, and docs/design/atomic-bus.md's Identity section makes no exception
// for it.
func chatAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus chat <room> [--as <name>] [--session <id>]\n"

	fs := flag.NewFlagSet("bus-chat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var as, session string
	fs.StringVar(&as, "as", "", "member name to claim in the room (default: $USER)")
	fs.StringVar(&session, "session", "", "override CLAUDE_CODE_SESSION_ID (scripted use, tests)")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room := positional[0]

	if as == "" {
		as = os.Getenv("USER")
	}
	if as == "" {
		fmt.Fprintln(os.Stderr, "atomic bus chat: --as is required ($USER is not set)")
		return int(ExitUsage)
	}

	sessionID, err := SessionID(session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}

	// Through the recoveryEnsurer seam, not the package-level EnsureDaemon —
	// see joinAction's identical comment; the two call sites share the same
	// fork-bomb hazard under `go test`.
	joinClient, err := recoveryEnsurer().EnsureDaemon(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}
	resp, err := joinClient.Do(Request{Op: OpJoin, Room: room, Name: as, Mode: "participate", Kind: KindHuman, Session: sessionID})
	joinClient.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}
	var joinPayload struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(resp.Payload, &joinPayload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: parse response: %v\n", err)
		return int(ExitHard)
	}
	name := joinPayload.Name

	st, err := Load(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return int(ExitHard)
	}
	st.Join(sessionID, room, name, "participate", KindHuman)
	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return int(ExitHard)
	}
	if name != as {
		fmt.Fprintf(out, "joined %s as %s (requested %s was taken)\n", room, name, as)
	} else {
		fmt.Fprintf(out, "joined %s as %s\n", room, name)
	}

	subClient, err := dialDaemonRecovered(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}
	// Session is set but SkipSelf is not: chat must keep seeing its own
	// lines — chat renders the operator's own line from this same
	// subscription's echo, so a self-skip would make chat go silent on the
	// operator's own input (docs/spec/atomic-bus.md: "tail and chat still
	// see the complete transcript including their own lines"). Setting
	// Session anyway is what lets Hub.Who attribute this subscription to
	// name's own liveness (hasLiveSubscription) — the same session
	// association item 2's self-echo fix introduced.
	envelopes, err := subClient.Subscribe(Request{Op: OpRecv, Room: room, Session: sessionID})
	if err != nil {
		subClient.Close()
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}
	defer subClient.Close()

	if restore := makeStdinRaw(); restore != nil {
		defer restore()
	}

	chat := &Chat{
		home:      home,
		room:      room,
		in:        os.Stdin,
		out:       out,
		colour:    isTerminalWriter(out),
		width:     terminalWidth(out),
		envelopes: envelopes,
		send:      chatSendFunc(home, room, sessionID),
		who:       chatWhoFunc(home, room),
		rooms:     chatRoomsFunc(home),
		halt:      chatHaltFunc(home, room),
		resume:    chatResumeFunc(home, room),
		leave:     chatLeaveFunc(home, room, sessionID),
	}

	if err := chat.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\natomic bus chat: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// makeStdinRaw puts a live terminal stdin into raw mode so Chat can decode
// and echo one keystroke at a time itself (see chat.go's Run doc) instead
// of the tty line-discipline buffering a whole line before this process
// ever sees it. Returns nil, doing nothing, when stdin is not a terminal
// (piped input, a non-interactive CI shell) — chat still works there, just
// without raw single-keystroke echo. The returned func restores the
// original terminal state; the caller must defer it.
func makeStdinRaw() func() {
	fd := os.Stdin.Fd()
	if !charmterm.IsTerminal(fd) {
		return nil
	}
	oldState, err := charmterm.MakeRaw(fd)
	if err != nil {
		return nil
	}
	return func() { _ = charmterm.Restore(fd, oldState) }
}

// chatSendFunc, chatWhoFunc, chatRoomsFunc, chatHaltFunc, chatResumeFunc,
// and chatLeaveFunc are chatAction's wiring of Chat's collaborator fields
// to the daemon — each its own named closure so chatAction's own body
// stays a flat sequence of setup steps rather than a wall of inline
// func literals.
func chatSendFunc(home, room, sessionID string) func(text string, to []string) error {
	return func(text string, to []string) error {
		_, err := doWithRecovery(home, Request{Op: OpSend, Room: room, Session: sessionID, To: to, Text: text})
		return err
	}
}

func chatWhoFunc(home, room string) func() ([]Member, error) {
	return func() ([]Member, error) {
		client, err := dialDaemonRecovered(home)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		resp, err := client.Do(Request{Op: OpWho, Room: room})
		if err != nil {
			return nil, err
		}
		var payload struct {
			Members []Member `json:"members"`
		}
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			return nil, fmt.Errorf("bus: parse who response: %w", err)
		}
		return payload.Members, nil
	}
}

func chatRoomsFunc(home string) func() ([]RoomInfo, error) {
	return func() ([]RoomInfo, error) {
		client, err := dialDaemonRecovered(home)
		if err != nil {
			return nil, err
		}
		defer client.Close()
		resp, err := client.Do(Request{Op: OpRooms})
		if err != nil {
			return nil, err
		}
		var payload struct {
			Rooms []RoomInfo `json:"rooms"`
		}
		if err := json.Unmarshal(resp.Payload, &payload); err != nil {
			return nil, fmt.Errorf("bus: parse rooms response: %w", err)
		}
		return payload.Rooms, nil
	}
}

func chatHaltFunc(home, room string) func(text string) error {
	return func(text string) error {
		client, err := dialDaemonRecovered(home)
		if err != nil {
			return err
		}
		defer client.Close()
		_, err = client.Do(Request{Op: OpHalt, Room: room, Text: text})
		return err
	}
}

func chatResumeFunc(home, room string) func() error {
	return func() error {
		client, err := dialDaemonRecovered(home)
		if err != nil {
			return err
		}
		defer client.Close()
		_, err = client.Do(Request{Op: OpResume, Room: room})
		return err
	}
}

func chatLeaveFunc(home, room, sessionID string) func() error {
	return func() error {
		if _, err := doWithRecovery(home, Request{Op: OpLeave, Room: room, Session: sessionID}); err != nil {
			return err
		}
		st, err := Load(home)
		if err != nil {
			return err
		}
		st.Leave(sessionID, room)
		return st.Save(home)
	}
}
