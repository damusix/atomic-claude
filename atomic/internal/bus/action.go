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
)

// BusAction is the exported entry point for `atomic bus`, mirroring
// internal/wiki's WikiAction (internal/wiki/action.go:17): home is injected
// rather than resolved via os.UserHomeDir() internally, so
// cmd/atomic/main.go's runBus owns the one os.UserHomeDir() call and every
// path in this package stays testable against a temp dir. cwd is accepted
// for signature parity with WikiAction; nothing in bus needs it today (no
// --root-style flag), so it is unused.
//
// tail, say, halt, resume, and chat are not wired here — checkpoints 5 and 6
// (docs/spec/atomic-bus.md) — so those verbs fall through to the unknown-verb
// case below like any other typo.
func BusAction(args []string, home, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic bus <join|leave|send|recv|who|rooms|status|serve> [flags]")
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
// implements IsBoolFlag() bool. Without this, "--follow" or "--json"
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
// (docs/spec/atomic-bus.md: "idle shutdown is invisible to a joined
// session"). Idle shutdown and `serve --stop` both tear the daemon process
// down along with its in-memory roster, but bus.json already holds every
// session's membership, and the respawned daemon's own Hub.Rehydrate call
// at Serve startup (see serveAction) restores that whole roster before it
// accepts a single connection — so recovery here is nothing more than
// getting a live daemon back; there is no client-side rejoin left to do
// (see docs/spec/atomic-bus.md's "the daemon rehydrates the roster"
// change-log entry, which replaced the per-session re-registration this
// used to do). EnsureDaemon owns its own bounded spawn-and-retry loop, so a
// daemon that still won't come back surfaces that terminal error directly
// — never a second recovery attempt on top of it.
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
// participate|observe] [--session <id>]`. The numeric-suffix retry on a name
// collision is Hub.Join's job (room.go) — this only reports the assigned
// name, which may differ from the one requested.
func joinAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus join <room> --as <name> [--mode participate|observe] [--session <id>]\n"

	fs := flag.NewFlagSet("bus-join", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var as, mode, session string
	fs.StringVar(&as, "as", "", "member name to claim in the room (required)")
	fs.StringVar(&mode, "mode", "participate", "participate or observe")
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

	resp, err := client.Do(Request{Op: OpJoin, Room: room, Name: as, Mode: mode, Kind: KindAgent, Session: sessionID})
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
	st.Join(sessionID, room, payload.Name, mode, KindAgent)
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

// leaveAction implements `atomic bus leave [<room>]`. A missing room
// defaults to the session's last-joined room via State.ResolveRoom;
// leaving clears local state for that room only, per the brief.
func leaveAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus leave [<room>]\n"

	fs := flag.NewFlagSet("bus-leave", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	positional := fs.Args()
	if len(positional) > 1 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	var explicit string
	if len(positional) == 1 {
		explicit = positional[0]
	}

	sessionID, err := SessionID("")
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
// <name>,...] [--reply-to <msg-id>]`.
func sendAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus send <room> <text> [--to <name>,...] [--reply-to <msg-id>] [--json]\n"

	fs := flag.NewFlagSet("bus-send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var to, replyTo string
	var jsonOut bool
	fs.StringVar(&to, "to", "", "comma-separated addressee names (omitted means FYI to the whole room)")
	fs.StringVar(&replyTo, "reply-to", "", "id of the message being replied to")
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

	sessionID, err := SessionID("")
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

// recvAction implements `atomic bus recv <room> [--follow] [--since
// <msg-id>] [--json]`. --follow always emits JSONL (the Monitor path); a
// one-shot recv renders a plain line per envelope by default and JSONL
// under --json — see docs/design/atomic-bus.md's "Ambiguity resolved in the
// contract". Table/colour rendering is checkpoint 5's render.go.
func recvAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus recv <room> [--follow] [--since <msg-id>] [--json]\n"

	fs := flag.NewFlagSet("bus-recv", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var follow, jsonOut bool
	var since string
	fs.BoolVar(&follow, "follow", false, "stream live JSONL until SIGTERM/SIGINT")
	fs.StringVar(&since, "since", "", "replay envelopes after this message id")
	fs.BoolVar(&jsonOut, "json", false, "emit JSONL for a one-shot recv (--follow always emits JSONL)")
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
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}

	if follow {
		return recvFollow(client, room, since, out)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpRecv, Room: room, Since: since})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}
	var payload struct {
		Envelopes []Envelope `json:"envelopes"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus recv: parse response: %v\n", err)
		return int(ExitHard)
	}

	if jsonOut {
		enc := json.NewEncoder(out)
		for _, env := range payload.Envelopes {
			if err := enc.Encode(env); err != nil {
				fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
				return int(ExitHard)
			}
		}
		return int(ExitOK)
	}

	for _, env := range payload.Envelopes {
		fmt.Fprintf(out, "%s\t%s\t%s\n", env.ID, env.From, env.Text)
	}
	return int(ExitOK)
}

// recvFollow is the Monitor path: one JSON envelope per line, flushed per
// line — json.Encoder.Encode issues exactly one Write per call, and out is
// the raw stdout stream with nothing buffering in front of it, so every
// line reaches the reader the instant it's written. Termination is checked
// only at loop-iteration boundaries (the select below), never mid-write, so
// a SIGTERM/SIGINT arriving while a line is being written cannot truncate
// it — the current write always completes before the next select decides
// whether to exit. A buffered or dropped line here is the entire recv
// --follow feature failing silently (docs/spec/atomic-bus.md).
func recvFollow(client *Client, room, since string, out io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ch, err := client.Subscribe(Request{Op: OpRecv, Room: room, Follow: true, Since: since})
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
		fmt.Fprintf(out, "%s\t%s\t%s\n", m.Name, m.Kind, m.Mode)
	}
	return int(ExitOK)
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

// statusAction implements `atomic bus status [--json]`: this session's
// joined rooms (from local state) plus the daemon's own reachability and
// identity. Unlike join, status never spawns a daemon — an unreachable
// daemon is exactly what status is for reporting, not a condition to fix.
func statusAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus status [--json]\n"

	fs := flag.NewFlagSet("bus-status", flag.ContinueOnError)
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

	sessionID, err := SessionID("")
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
// this; recv's --json path is JSONL instead, one envelope per line, to
// match --follow's wire shape.
func emitJSON(out io.Writer, v any) int {
	if err := json.NewEncoder(out).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus: encode JSON: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// serveAction implements `atomic bus serve [--idle-shutdown-minutes N]
// [--stop]`. Without --stop it runs the daemon in the foreground — this is
// exactly what EnsureDaemon spawns (client.go's spawnServe) — binding the
// real socket, owning the socket file and this invocation's share of the
// spawn-lock protocol (Serve itself only owns what happens once it has a
// live listener; see daemon.go's Serve doc). --stop instead sends the wire
// shutdown op to a daemon that's already running.
func serveAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus serve [--idle-shutdown-minutes N] [--stop]\n"

	fs := flag.NewFlagSet("bus-serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var idleMinutes int
	var stopFlag bool
	fs.IntVar(&idleMinutes, "idle-shutdown-minutes", int(DefaultIdleWindow/time.Minute), "idle-shutdown window in minutes (0 disables)")
	fs.BoolVar(&stopFlag, "stop", false, "stop a running daemon and exit")
	if err := fs.Parse(args); err != nil {
		return int(ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}

	if stopFlag {
		return serveStop(home, out)
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
	idleWindow := time.Duration(idleMinutes) * time.Minute

	// nil == ok: Serve returns nil on a wire shutdown (--stop) or an idle
	// timeout, and context.Canceled on our own signal-driven ctx — both are
	// a clean stop, not a failure to report.
	if err := Serve(ctx, ln, hub, idleWindow, nil); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "atomic bus serve: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// serveStop sends the shutdown op to a running daemon. No daemon running is
// treated as already-stopped (exit 0, not an error): --stop's job is to
// reach the goal state "no daemon", which a missing daemon has already
// reached — this is the documented remedy the version-skew error message
// points users at, so it must succeed even when there is nothing to stop.
func serveStop(home string, out io.Writer) int {
	client, err := Dial(home, defaultDialTimeout)
	if err != nil {
		fmt.Fprintln(out, "no daemon running")
		return int(ExitOK)
	}
	defer client.Close()

	if _, err := client.Do(Request{Op: OpShutdown}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus serve --stop: %v\n", err)
		return exitFromErr(err)
	}
	fmt.Fprintln(out, "daemon stopped")
	return int(ExitOK)
}
