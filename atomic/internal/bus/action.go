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

// BusAction is the exported entry point for `atomic bus`. home and cwd are
// injected rather than resolved here, so cmd/atomic/main.go's runBus owns the
// one os.UserHomeDir call and every path in this package stays testable
// against a temp dir.
func BusAction(args []string, home, cwd string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: atomic bus <join|leave|send|recv|who|rooms|status|serve|start|stop|restart|tail|say|read|halt|resume|prune|close|chat> [flags]")
		return int(ExitUsage)
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "join":
		return joinAction(rest, home, cwd, out)
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
	case "read":
		return readAction(rest, home, out)
	case "halt":
		return haltAction(rest, home, out)
	case "resume":
		return resumeAction(rest, home, out)
	case "prune":
		return pruneAction(rest, home, out)
	case "close":
		return closeAction(rest, home, out)
	case "chat":
		return chatAction(rest, home, cwd, out)
	default:
		fmt.Fprintf(os.Stderr, "atomic bus: unknown verb %q\n", verb)
		return int(ExitUsage)
	}
}

// parseFlags parses args against fs and returns the positionals, allowing
// flags and positionals in any order. flag.FlagSet.Parse stops at the first
// non-flag token and leaves the rest — including later flags — in Args(), so a
// single fs.Parse(args) would silently leave --as unparsed.
//
// A token counts as a flag only when shaped "--name" or "--name=value" AND
// registered on fs; every flag in this package is long-form, so that is the
// whole grammar here. Anything else — a single-dash token, the "-" stdin
// sentinel, a positional that happens to start with "-" — never reaches
// fs.Parse and so cannot trip its "flag provided but not defined" rejection.
// A bare "--" ends flag scanning. An unregistered "--name" is still a hard
// error, delegated to fs.Parse for its standard message.
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
			// fs.Parse gives the standard "flag provided but not defined" error.
			return nil, fs.Parse([]string{arg})
		}
		if strings.Contains(arg, "=") || isBoolFlag(f) {
			// "--name=value" is self-contained; a bool flag takes no separate
			// value token (mirrors flag.FlagSet's own grammar).
			if err := fs.Parse([]string{arg}); err != nil {
				return nil, err
			}
			i++
			continue
		}
		if i+1 >= len(args) {
			// No value token left: fs.Parse produces "flag needs an argument".
			return nil, fs.Parse([]string{arg})
		}
		// The next token is this flag's value verbatim, even if it starts with "-".
		if err := fs.Parse([]string{arg, args[i+1]}); err != nil {
			return nil, err
		}
		i += 2
	}
	return positional, nil
}

// flagName reports whether arg is shaped "--name" or "--name=value" — the only
// flag shape this package registers — and returns the bare name. Any other
// shape returns ok=false so the caller treats arg as positional.
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

// isBoolFlag reports whether f takes no argument, via the same IsBoolFlag()
// interface flag.FlagSet checks internally. Without it, "--json" would swallow
// the next positional as its value.
func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// exitFromErr maps err to a process exit code: a *bus.Error's own Code when it
// carries one, else ExitHard. Every action routes through this rather than
// re-deriving a code from error text.
func exitFromErr(err error) int {
	var busErr *Error
	if errors.As(err, &busErr) {
		return int(busErr.Code)
	}
	return int(ExitHard)
}

// dialDaemon connects without spawning. Every verb except join assumes join
// already spawned a daemon — spawning here would mean a stray `atomic bus who`
// on a cold machine brings one up just to answer a query. Dial's failure is a
// plain error, not a *bus.Error, so it is remapped to ExitUnreachable rather
// than falling through exitFromErr's ExitHard default.
func dialDaemon(home string) (*Client, error) {
	client, err := Dial(home, defaultDialTimeout)
	if err != nil {
		return nil, &Error{Code: ExitUnreachable, Msg: fmt.Sprintf("bus: daemon unreachable: %v", err)}
	}
	return client, nil
}

// recoveryEnsurer is a testable seam: tests substitute an Ensurer whose Spawn
// starts an in-process daemon instead of depending on a real `atomic` binary.
var recoveryEnsurer = DefaultEnsurer

// dialDaemonRecovered dials and, only when the daemon is unreachable, respawns
// it and retries exactly once. Recovery is nothing more than getting a live
// daemon back: bus.json already holds every membership, and the respawned
// daemon's Hub.Rehydrate restores the whole roster before it accepts a
// connection, so there is no client-side rejoin left to do. EnsureDaemon owns
// its own bounded spawn-and-retry, so a daemon that still won't come back
// surfaces that terminal error rather than a second recovery attempt.
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

// doWithRecovery performs req against a daemon guaranteed live. Used only by
// send and leave, the two ops that require existing membership. ExitNotJoined
// is passed through untouched, never treated as a recovery symptom: a
// restarted daemon knows every persisted member the instant it serves, so
// ExitNotJoined means exactly what it says.
func doWithRecovery(home string, req Request) (Response, error) {
	client, err := dialDaemonRecovered(home)
	if err != nil {
		return Response{}, err
	}
	defer client.Close()
	return client.Do(req)
}

// touchLastSeen best-effort persists that session was active in room — the
// disk-side half of Hub.Publish's in-memory LastSeen refresh. A failure here
// is not a command failure: the message was already delivered, and losing this
// write only leaves the next restart's staleness read a beat behind.
func touchLastSeen(home, session, room string, now time.Time) {
	st, err := Load(home)
	if err != nil {
		return
	}
	if !st.TouchLastSeen(session, room, now) {
		return
	}
	_ = st.Save(home)
}

// joinAction implements `atomic bus join`. The numeric-suffix retry on a name
// collision is Hub.Join's job; this only reports the assigned name, which may
// differ from the one requested. --kind defaults to agent, so a person joining
// from a terminal passes --kind human and from_kind-keyed reaction-policy rules
// fire for them.
//
// A member's name is its resolved position stacked with an optional role suffix
// — the name is the position, --as is only the role — so omitting --as still
// yields a usable deterministic name rather than making it a required flag.
// Position is resolved regardless, since Member.Repo/Realm are recorded on
// every join.
func joinAction(args []string, home, cwd string, out io.Writer) int {
	const usage = "Usage: atomic bus join <room> [--as <role>] [--mode participate|observe] [--kind agent|human] [--session <id>]\n"

	fs := flag.NewFlagSet("bus-join", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var as, mode, kind, session string
	fs.StringVar(&as, "as", "", "role suffix on the derived position name (optional)")
	fs.StringVar(&mode, "mode", "participate", "participate or observe")
	fs.StringVar(&kind, "kind", KindAgent, "agent or human")
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

	pos, err := resolvePosition(home, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return int(ExitHard)
	}
	name := pos.name(as)

	// Through the recoveryEnsurer seam, never the package-level EnsureDaemon:
	// the bare call bypasses the injection point, so a test exercising join
	// reaches the real spawnServe, which re-execs the test binary — a fork bomb.
	client, err := recoveryEnsurer().EnsureDaemon(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpJoin, Room: room, Name: name, Mode: mode, Kind: kind, Session: sessionID, Repo: pos.repo, Realm: pos.realm})
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
	st.Join(sessionID, room, payload.Name, mode, kind, pos.repo, pos.realm)
	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus join: %v\n", err)
		return int(ExitHard)
	}

	if payload.Name != name {
		fmt.Fprintf(out, "joined %s as %s (requested %s was taken)\n", room, payload.Name, name)
	} else {
		fmt.Fprintf(out, "joined %s as %s\n", room, payload.Name)
	}
	return int(ExitOK)
}

// leaveAction implements `atomic bus leave [<room>]`. A missing room defaults
// to the session's last-joined room; leaving clears local state for that room
// only.
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

	resp, err := doWithRecovery(home, Request{Op: OpLeave, Room: room, Session: sessionID})
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return exitFromErr(err)
	}

	st.Leave(sessionID, room)

	// If this leave emptied the room and the daemon dropped it, clear any
	// orphaned persisted halt state too — otherwise a later Rehydrate would
	// resurrect an unoccupied room, halted for a reason nobody can act on.
	var leavePayload struct {
		RoomDropped bool `json:"room_dropped,omitempty"`
	}
	if json.Unmarshal(resp.Payload, &leavePayload) == nil && leavePayload.RoomDropped {
		delete(st.Rooms, room)
	}

	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus leave: %v\n", err)
		return int(ExitHard)
	}

	fmt.Fprintf(out, "left %s\n", room)
	return int(ExitOK)
}

// sendAction implements `atomic bus send <room> <text>`.
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

	// Still delivered, and this still exits 0 — a named addressee may be about
	// to join — but the sender must not be told nothing.
	if len(payload.UnknownTo) > 0 {
		fmt.Fprintf(os.Stderr, "atomic bus send: warning: not currently in room %s: %s\n", room, strings.Join(payload.UnknownTo, ", "))
	}

	touchLastSeen(home, sessionID, room, time.Now())

	if jsonOut {
		return emitJSON(out, payload.Envelope)
	}
	// A bare id is noise for a human and under-structured for an agent capturing
	// it; --json is the structured path.
	fmt.Fprintf(out, "sent to %s (id %s)\n", room, payload.Envelope.ID)
	return int(ExitOK)
}

// readText returns text verbatim, or all of stdin when text is "-" — the path
// for a multi-line payload that must not pass through shell quoting.
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

// parseTo splits a comma-separated --to value. An empty string yields nil, not
// [""], because nil is what marks an envelope as an FYI to the whole room.
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

// recvAction implements `atomic bus recv <room>`. recv always streams: one JSON
// envelope per line, exiting 0 on SIGTERM. There is no one-shot mode and no
// --follow flag to forget — a recv that returned would leave a Monitor silently
// hearing nothing. --json is accepted for consistency but is a no-op.
//
// recv resolves its own session identity so the daemon can suppress this
// subscriber's own publishes (SkipSelf); self-echo would otherwise cost the
// agent a wasted prompt per message it sends. No resolvable session fails
// exactly like send/leave/join rather than silently degrading that suppression.
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
	return recvStream(client, home, room, sessionID, out)
}

// recvStream is the Monitor path: one JSON envelope per line, flushed per line
// — json.Encoder.Encode issues exactly one Write per call and nothing buffers
// in front of out. Termination is checked only at loop boundaries, never
// mid-write, so a signal cannot truncate a line. A buffered or dropped line
// here is the entire recv feature failing silently. No backlog is replayed.
// SkipSelf is always set: this session's own sends are exactly what a recv
// subscriber must not see back.
//
// The loop reconnects rather than exiting whenever the channel closes for any
// reason other than ctx cancellation or an explicit room close
// (Envelope.Closing). A daemon restart is indistinguishable at this layer from
// any dropped connection, and both used to make recv exit 0 while the Monitor
// reported a clean end and the roster kept listing this member as live — a
// deaf session peers keep addressing. A genuine reconnect failure returns
// non-zero so the Monitor surfaces the fault instead of a silent 0.
func recvStream(client *Client, home, room, sessionID string, out io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	enc := json.NewEncoder(out)

	ch, err := client.Subscribe(Request{Op: OpRecv, Room: room, Session: sessionID, SkipSelf: true})
	if err != nil {
		client.Close()
		fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
		return exitFromErr(err)
	}

	for {
		reconnect, code := recvDeliver(ctx, ch, enc)
		client.Close()
		if !reconnect {
			return code
		}

		// A stop signal arriving in the gap before a reconnect must win
		// immediately, not after a full dial-and-subscribe attempt (which spans
		// EnsureDaemon's own timeouts). recvDeliver's select covers ctx firing
		// while a subscription is live; this covers the gap after one ends.
		select {
		case <-ctx.Done():
			return int(ExitOK)
		default:
		}

		fmt.Fprintln(os.Stderr, "atomic bus recv: lost connection to the daemon, reconnecting...")
		client, ch, err = dialAndSubscribeRecv(home, room, sessionID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic bus recv: could not reconnect: %v\n", err)
			return exitFromErr(err)
		}
	}
}

// reconnectAttempts bounds dialAndSubscribeRecv's retry — retry once, then
// fail clearly rather than loop forever.
const reconnectAttempts = 2

// reconnectRetryDelay is the pause between dialAndSubscribeRecv's attempts:
// long enough for the previous daemon's shutdown to genuinely finish, short
// enough not to matter to a waiting listener.
const reconnectRetryDelay = 50 * time.Millisecond

// dialAndSubscribeRecv dials and subscribes, retrying the whole pair once. Not
// about spawning a fresh daemon twice — dialDaemonRecovered already retries a
// stale-socket dial — but about a race a plain connect cannot see: a reconnect
// can land in the window where the previous daemon has accepted the connection
// but is mid-shutdown, so it closes without answering the subscribe handshake.
// That is indistinguishable from a dead daemon, and a stale dial can hand back
// the same dying process twice, which is why the pair and not just Subscribe
// is retried.
func dialAndSubscribeRecv(home, room, sessionID string) (*Client, <-chan Envelope, error) {
	var lastErr error
	for attempt := 0; attempt < reconnectAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(reconnectRetryDelay)
		}
		client, err := dialDaemonRecovered(home)
		if err != nil {
			lastErr = err
			continue
		}
		ch, err := client.Subscribe(Request{Op: OpRecv, Room: room, Session: sessionID, SkipSelf: true})
		if err != nil {
			client.Close()
			lastErr = err
			continue
		}
		return client, ch, nil
	}
	return nil, nil, lastErr
}

// recvDeliver runs one subscription's delivery loop and reports whether
// recvStream should reconnect: true exactly when ch closed without ctx being
// cancelled first and the last envelope was not Hub.Close's closing envelope.
// Without the Closing check a close is indistinguishable from a daemon restart
// here, and recv would reconnect to — and recreate — the room just closed.
func recvDeliver(ctx context.Context, ch <-chan Envelope, enc *json.Encoder) (reconnect bool, code int) {
	closing := false
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				return !closing, int(ExitOK)
			}
			closing = env.Closing
			if err := enc.Encode(env); err != nil {
				fmt.Fprintf(os.Stderr, "atomic bus recv: %v\n", err)
				return false, int(ExitHard)
			}
		case <-ctx.Done():
			return false, int(ExitOK)
		}
	}
}

// whoAction implements `atomic bus who [<room>]`. An explicitly named room
// needs no session identity, so an operator can inspect any room from outside a
// live Claude Code session.
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
	var payload whoJSON
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus who: parse response: %v\n", err)
		return int(ExitHard)
	}

	if jsonOut {
		return emitJSON(out, payload)
	}
	if payload.Halted {
		fmt.Fprintf(out, "room %s is HALTED%s\n", room, haltReasonNote(payload.HaltReason))
	}
	for _, m := range payload.Members {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n", m.Name, m.Kind, m.Mode, livenessLabel(m.Stale), m.Repo, m.Realm)
	}
	return int(ExitOK)
}

// haltReasonNote renders " (why)" for a non-empty halt reason, else "".
func haltReasonNote(reason string) string {
	if reason == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", reason)
}

// livenessLabel renders Member.Stale for plain text — shared with render.go's
// MemberTable so `who` and chat's `/who` agree on the same two words.
func livenessLabel(stale bool) string {
	if stale {
		return "stale"
	}
	return "live"
}

// resolveOptionalRoom returns explicit verbatim, or falls back to the session's
// last-joined room. Session identity is needed only in that fallback, so naming
// a room outright works outside a live Claude Code session.
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
		fmt.Fprintf(out, "%s\t%d%s\n", r.Name, r.Members, haltedSuffix(r.Halted, r.HaltReason))
	}
	return int(ExitOK)
}

// haltedSuffix renders a trailing " [HALTED: <reason>]" marker, else "" —
// shared by rooms and status so an operator who halted a room and walked away
// can tell from either.
func haltedSuffix(halted bool, reason string) string {
	if !halted {
		return ""
	}
	if reason == "" {
		return " [HALTED]"
	}
	return fmt.Sprintf(" [HALTED: %s]", reason)
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

// joinedRoomStatus is one entry of busStatus.Rooms: the room, the name this
// session holds in it (a join may have been renamed), and its halt state.
type joinedRoomStatus struct {
	Room       string `json:"room"`
	Name       string `json:"name"`
	Halted     bool   `json:"halted,omitempty"`
	HaltReason string `json:"halt_reason,omitempty"`
}

// statusAction implements `atomic bus status`: this session's joined rooms plus
// the daemon's own reachability and identity. Unlike join it never spawns — an
// unreachable daemon is what status is for reporting, not a condition to fix.
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
		resp, perr := client.Do(Request{Op: OpPing})
		client.Close()
		if perr == nil {
			var payload struct {
				Version int `json:"version"`
				Pid     int `json:"pid"`
			}
			if json.Unmarshal(resp.Payload, &payload) == nil {
				status.Daemon = "running"
				status.Version = payload.Version
				status.Pid = payload.Pid
				status.Rooms = annotateHalted(home, status.Rooms)
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
			fmt.Fprintf(out, "%s\t%s%s\n", r.Room, r.Name, haltedSuffix(r.Halted, r.HaltReason))
		}
	}
	return int(ExitOK)
}

// annotateHalted fills in Halted/HaltReason by fetching the daemon's `rooms`
// list and matching by name. A second round trip because Client.Do consumes its
// connection and OpPing's payload carries no per-room data. Best-effort: any
// failure leaves rooms exactly as passed in, since status's primary job is
// reporting reachability, not halt state.
func annotateHalted(home string, rooms []joinedRoomStatus) []joinedRoomStatus {
	client, err := dialDaemon(home)
	if err != nil {
		return rooms
	}
	defer client.Close()

	resp, err := client.Do(Request{Op: OpRooms})
	if err != nil {
		return rooms
	}
	var payload struct {
		Rooms []RoomInfo `json:"rooms"`
	}
	if json.Unmarshal(resp.Payload, &payload) != nil {
		return rooms
	}
	byName := make(map[string]RoomInfo, len(payload.Rooms))
	for _, r := range payload.Rooms {
		byName[r.Name] = r
	}
	for i := range rooms {
		if info, ok := byName[rooms[i].Room]; ok {
			rooms[i].Halted = info.Halted
			rooms[i].HaltReason = info.HaltReason
		}
	}
	return rooms
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

// emitJSON writes v as a single JSON value plus a newline — the snapshot verbs
// (who, rooms, status). recv streams JSONL instead; see recvStream.
func emitJSON(out io.Writer, v any) int {
	if err := json.NewEncoder(out).Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus: encode JSON: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// serveAction implements `atomic bus serve`: runs the daemon in the foreground,
// which is exactly what EnsureDaemon spawns. No idle-shutdown timer and no
// --stop flag — start, stop, and restart control the daemon explicitly.
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
	// A restarted daemon must come back with the whole persisted roster, not
	// rebuild it one session at a time as each runs a command. This runs before
	// Serve's accept loop starts. A missing bus.json is not an error; a malformed
	// one degrades to an empty roster rather than blocking startup.
	if st, err := Load(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus serve: warning: could not load %s, starting with an empty roster: %v\n", StatePath(home), err)
	} else {
		hub.Rehydrate(st)
	}

	// Serve returns nil on a wire shutdown and context.Canceled on our own
	// signal-driven ctx — both are a clean stop, not a failure to report.
	if err := Serve(ctx, ln, hub, nil); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "atomic bus serve: %v\n", err)
		return int(ExitHard)
	}
	return int(ExitOK)
}

// startAction implements `atomic bus start`: spawns the daemon if none is
// listening. Idempotent, and goes through the same recoveryEnsurer seam every
// other recovery path uses, so there is exactly one spawn implementation.
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
// listening — used only to choose start's message. EnsureDaemon's flock is what
// makes concurrent starts idempotent, not this probe: two racing starts can
// both print "daemon started" while only one of them spawns.
func probeRunning(home string) bool {
	client, err := dialDaemon(home)
	if err != nil {
		return false
	}
	defer client.Close()
	return checkVersion(client) == nil
}

// stopAction implements `atomic bus stop`. No daemon running is treated as
// already-stopped (exit 0): stop's job is the goal state "no daemon", which a
// missing daemon has reached — and the version-skew error points users here, so
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

// restartWaitTimeout bounds restartAction's wait for stop's socket teardown.
// Serve's listener Close and its unlink run asynchronously after the shutdown
// op's reply is sent, so a start immediately after stop can race the socket
// file.
const restartWaitTimeout = 2 * time.Second

// restartAction implements `atomic bus restart`: stop then start. Works whether
// or not a daemon is running, since stopAction's "no daemon" case is exit 0.
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

// waitForSocketGone polls the socket until it refuses connections or timeout
// elapses. Best-effort: a timeout still lets EnsureDaemon's own stale-socket
// recovery reach a live daemon; this only avoids relying on that retry.
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

// haltAction implements `atomic bus halt <room>`. Halt is enforced server-side
// (Hub.Publish); this only sends the wire op, and needs no session identity
// since an operator can halt a room they are not in.
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

	if err := persistHalted(home, room, true, text); err != nil {
		// The halt already succeeded against the daemon; only durability
		// across a restart is at risk, so this is a warning, not a failure.
		fmt.Fprintf(os.Stderr, "atomic bus halt: warning: halt succeeded but was not persisted (a daemon restart would lose it): %v\n", err)
	}

	fmt.Fprintf(out, "halted %s\n", room)
	return int(ExitOK)
}

// persistHalted records room's halt flag and reason in bus.json — the durable
// half of Hub.Halt/Hub.Resume, which only mutate the daemon's in-memory Room.
func persistHalted(home, room string, halted bool, text string) error {
	st, err := Load(home)
	if err != nil {
		return err
	}
	st.SetHalted(room, halted, text)
	return st.Save(home)
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

	if err := persistHalted(home, room, false, ""); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus resume: warning: resume succeeded but the cleared halt state was not persisted: %v\n", err)
	}

	fmt.Fprintf(out, "resumed %s\n", room)
	return int(ExitOK)
}

// --- prune ---

// pruneAction implements `atomic bus prune [<room>]`. Removes only members
// Hub.Prune finds stale, and only when an operator asks: a quiet session is not
// a dead one, and evicting a live member breaks addressing with no diagnostic.
// A missing room defaults to the session's last-joined room, same as who.
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

// --- close ---

// closeAction implements `atomic bus close <room>`: operator-level teardown, no
// session identity required. Drops the room server-side (Hub.Close), then
// clears its memberships and halt state from bus.json so a restart does not
// rebuild it — the local half Hub.Close cannot do, bus.json being client-side.
func closeAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus close <room>\n"

	fs := flag.NewFlagSet("bus-close", flag.ContinueOnError)
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
		fmt.Fprintf(os.Stderr, "atomic bus close: %v\n", err)
		return exitFromErr(err)
	}
	defer client.Close()

	if _, err := client.Do(Request{Op: OpClose, Room: room}); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus close: %v\n", err)
		return exitFromErr(err)
	}

	st, err := Load(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus close: %v\n", err)
		return int(ExitHard)
	}
	st.ClearRoom(room)
	if err := st.Save(home); err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus close: %v\n", err)
		return int(ExitHard)
	}

	fmt.Fprintf(out, "closed %s\n", room)
	return int(ExitOK)
}

// --- say ---

// sayAction implements `atomic bus say <room> <text>`. Publishes via
// Hub.PublishAsOperator, which needs no prior join and passes even into a
// halted room — the asymmetry that makes halt useful for an operator.
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

	// No Name or Kind: the daemon pins the operator identity and ignores both on
	// OpSay. Sending them would imply the client gets a say in who it publishes
	// as, which is exactly the trust the daemon must not extend.
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

	// Mirrors sendAction's warn-but-still-send contract.
	if len(payload.UnknownTo) > 0 {
		fmt.Fprintf(os.Stderr, "atomic bus say: warning: not currently in room %s: %s\n", room, strings.Join(payload.UnknownTo, ", "))
	}

	fmt.Fprintf(out, "said to %s (id %s)\n", room, payload.Envelope.ID)
	return int(ExitOK)
}

// --- tail ---

// isTerminalWriter reports whether out is a live terminal — TailLine's colour
// switch. Anything that is not an *os.File counts as non-tty, which is also the
// right answer for a redirected or piped os.Stdout.
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

// resolveTailRooms decides which rooms tail subscribes to and whether each line
// needs a room prefix. An explicit room subscribes to exactly that, unprefixed.
// Otherwise every known room is queried: a bare `tail` with exactly one room,
// or --all-rooms, subscribes to all of them, prefixed. Any other room count
// with neither is ambiguous and refused rather than silently guessed.
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

// tailAction implements `atomic bus tail [<room>]`. tail never joins — it does
// not occupy a name and does not appear in `who`, so any number of operators
// can watch one room at once. Like recv it delivers only what is published
// after it subscribes; there is no --since.
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

// tailStream is tailAction's subscription loop, factored out so tests can drive
// it against an already-connected *Client — closing the client is the only way
// to end a subscription loop deterministically. Filtering happens client-side:
// the daemon delivers every envelope unfiltered, so two operators tailing the
// same room with different filters never affect each other.
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

// readAction implements `atomic bus read <room> <msg-id>`: fetch one full
// envelope from the durable log. A pure log read — no daemon round trip, works
// with the daemon down. The recovery verb for a consumer whose notification
// layer truncated a message; the log line always carries the complete text.
func readAction(args []string, home string, out io.Writer) int {
	const usage = "Usage: atomic bus read <room> <msg-id> [--json]\n"

	fs := flag.NewFlagSet("bus-read", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var jsonOut bool
	fs.BoolVar(&jsonOut, "json", false, "emit the raw envelope JSON")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return int(ExitUsage)
	}
	if len(positional) != 2 {
		fmt.Fprint(os.Stderr, usage)
		return int(ExitUsage)
	}
	room, id := positional[0], positional[1]
	// Room names are free text on the wire, but this verb splices one into a
	// filesystem path — reject anything path-shaped.
	if room == "" || strings.ContainsAny(room, `/\`) || strings.Contains(room, "..") {
		fmt.Fprintf(os.Stderr, "atomic bus read: invalid room name %q\n", room)
		return int(ExitUsage)
	}

	env, found, err := ReadEnvelope(home, room, id)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "atomic bus read: no log for room %q\n", room)
			return int(ExitNoRoom)
		}
		fmt.Fprintf(os.Stderr, "atomic bus read: %v\n", err)
		return int(ExitHard)
	}
	if !found {
		fmt.Fprintf(os.Stderr, "atomic bus read: no message %q in room %q\n", id, room)
		return int(ExitHard)
	}

	if jsonOut {
		if err := json.NewEncoder(out).Encode(env); err != nil {
			fmt.Fprintf(os.Stderr, "atomic bus read: %v\n", err)
			return int(ExitHard)
		}
		return int(ExitOK)
	}
	// Full fidelity is this verb's whole point, and TailLine's collapse elides
	// anything past ~15 lines by design.
	addressee := "(fyi)"
	if len(env.To) > 0 {
		addressee = "to " + strings.Join(env.To, ", ")
	}
	header := fmt.Sprintf("%s  %s (%s) %s  [%s]", env.Ts.Format("2006-01-02 15:04:05"), env.From, env.FromKind, addressee, env.ID)
	if env.ReplyTo != "" {
		header += "  reply-to " + env.ReplyTo
	}
	fmt.Fprintf(out, "%s\n%s\n", header, env.Text)
	return int(ExitOK)
}

// --- chat ---

// chatAction implements `atomic bus chat <room>`: an interactive client that
// joins room as a KindHuman member and hands off to Chat's loop (chat.go)
// against a raw-mode stdin and the daemon's live subscription. --as defaults to
// $USER — chat's identity is the operator's own username, not the repo it is
// run from — while position (repo/realm) is still resolved and recorded, same
// as join.
func chatAction(args []string, home, cwd string, out io.Writer) int {
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

	pos, err := resolvePosition(home, cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return int(ExitHard)
	}

	// Through the recoveryEnsurer seam — see joinAction's identical comment;
	// both call sites share the same fork-bomb hazard under `go test`.
	joinClient, err := recoveryEnsurer().EnsureDaemon(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus chat: %v\n", err)
		return exitFromErr(err)
	}
	resp, err := joinClient.Do(Request{Op: OpJoin, Room: room, Name: as, Mode: "participate", Kind: KindHuman, Session: sessionID, Repo: pos.repo, Realm: pos.realm})
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
	st.Join(sessionID, room, name, "participate", KindHuman, pos.repo, pos.realm)
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
	// Session set but SkipSelf not: chat renders the operator's own line from
	// this subscription's echo, so a self-skip would make chat go silent on its
	// own input. Setting Session anyway is what lets Hub.Who attribute this
	// subscription to name's own liveness (hasLiveSubscription).
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

// makeStdinRaw puts a live terminal stdin into raw mode so Chat can decode and
// echo one keystroke at a time itself instead of the tty line discipline
// buffering a whole line first. Returns nil when stdin is not a terminal —
// chat still works there, without single-keystroke echo. The returned func
// restores the original terminal state; the caller must defer it.
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

// chatSendFunc and its siblings wire Chat's collaborator fields to the daemon,
// each a named closure so chatAction's body stays a flat sequence of setup
// steps rather than a wall of inline func literals.
func chatSendFunc(home, room, sessionID string) func(text string, to []string) error {
	return func(text string, to []string) error {
		_, err := doWithRecovery(home, Request{Op: OpSend, Room: room, Session: sessionID, To: to, Text: text})
		if err != nil {
			return err
		}
		// chat sends through this closure, not sendAction, so it needs its own
		// LastSeen refresh (see touchLastSeen).
		touchLastSeen(home, sessionID, room, time.Now())
		return nil
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
