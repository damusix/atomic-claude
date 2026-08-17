// Package bus implements atomic bus: a per-user daemon behind a Unix domain
// socket that lets concurrent Claude Code sessions on one machine message
// each other over named rooms. See docs/design/atomic-bus.md for the wire
// protocol rationale and docs/spec/atomic-bus.md for the implementation
// contract.
package bus

import (
	"encoding/json"
	"time"
)

// ProtocolVersion gates the client/daemon handshake. A client whose version
// differs from the running daemon's refuses to proceed rather than risk
// talking a wire format the other side doesn't understand.
//
// Bump this whenever the wire shape changes. Six such commits once landed
// against version 1 without a bump; TestProtocolWireShape_GoldenFieldsAndOps
// is what makes that fail a test instead of shipping silently.
const ProtocolVersion = 2

// Op names, the contract for what a Request.Op may be. Every op and its
// request/reply shape is documented in docs/design/atomic-bus.md.
// AllOps is the same list as a slice — keep both in sync;
// TestProtocolWireShape_GoldenFieldsAndOps pins AllOps against a golden
// list, so adding an op here without adding it there fails that test.
const (
	OpPing     = "ping"
	OpJoin     = "join"
	OpLeave    = "leave"
	OpSend     = "send"
	OpSay      = "say"
	OpRecv     = "recv"
	OpTail     = "tail"
	OpWho      = "who"
	OpRooms    = "rooms"
	OpHalt     = "halt"
	OpResume   = "resume"
	OpShutdown = "shutdown"
	OpPrune    = "prune"
	OpClose    = "close"
)

// AllOps lists every Request.Op this daemon accepts. daemon.go's "unknown
// op" error enumerates it, so the list is load-bearing production content,
// not merely a test fixture.
var AllOps = []string{
	OpPing, OpJoin, OpLeave, OpSend, OpSay, OpRecv, OpTail, OpWho, OpRooms,
	OpHalt, OpResume, OpShutdown, OpPrune, OpClose,
}

// Request is a single client-to-daemon frame: an op plus whichever operand
// fields that op uses. Unused fields are left zero and omitted from the
// wire — one struct covers every op rather than one type per op, since the
// protocol is small and an op switch on the daemon side already discriminates
// which fields apply.
type Request struct {
	Op string `json:"op"`

	Room    string   `json:"room,omitempty"`
	Rooms   []string `json:"rooms,omitempty"`
	Name    string   `json:"name,omitempty"`
	Mode    string   `json:"mode,omitempty"`
	Kind    string   `json:"kind,omitempty"`
	Session string   `json:"session,omitempty"`
	To      []string `json:"to,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
	Text    string   `json:"text,omitempty"`

	// Repo and Realm are the joining client's own position, reported once
	// at join like Mode and Kind — the daemon has no cwd of its own to
	// resolve these against, so this is client-reported input (see
	// position.go's resolvePosition). Hub.Join stores them on the roster.
	// They carry no weight on any other op: a send/say request setting
	// these has no effect — the envelope's from_repo/from_realm are always
	// stamped from the roster entry, never from the wire (see room.go's
	// Publish doc and the same invariant that already governs From/FromKind).
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`

	// SkipSelf opts an OpRecv subscription out of receiving envelopes
	// published by this same Session — the per-subscription flag `recv`
	// sets and `tail`/`chat` do not, since those want the whole transcript
	// including their own lines. Meaningless
	// without Session also being set (there is nothing to compare
	// against), and ignored entirely for OpTail, which never carries an
	// identity to skip.
	SkipSelf bool `json:"skip_self,omitempty"`

	// Filters narrows a tail subscription (e.g. "only_addressed", "from").
	// Kept as a generic map rather than a named type so the render/action
	// consumer can add filter keys without another protocol.go amendment.
	Filters map[string]string `json:"filters,omitempty"`
}

// Response is a single daemon-to-client frame answering a Request, or (for
// the three subscription ops — recv --follow, tail, chat) the frame that
// opens a subscription before Envelope frames start arriving.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	// Code is the exit code the client should terminate with on failure.
	// Error-to-exit-code mapping lives here, in the daemon that produces
	// the response, rather than being re-derived from Error's text on the
	// client — one place decides what a failure means.
	Code ExitCode `json:"code,omitempty"`

	// Payload carries whatever reply shape the op produced (ping's
	// version/pid/started, join's assigned name, send's id, who's
	// members, rooms' room list, ...). Kept generic here so protocol.go
	// does not need one struct per op; the daemon (encode) and client
	// (decode) checkpoints define and share the concrete per-op shapes.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MaxTextBytes bounds Envelope.Text as enforced by Hub.Publish: a message
// over this limit is rejected with ExitUsage rather than written, so a room
// log line can never grow unbounded.
const MaxTextBytes = 1024 * 1024

// MaxIdentifierBytes bounds Room and a member's assigned Name (both
// enforced by Hub.Join) and ReplyTo (enforced by Hub.Publish) — the other
// string-typed envelope metadata fields roomlog.go's scanner budget has to
// hold room for alongside Text. See roomlog.go's scannerMaxLineBytes.
const MaxIdentifierBytes = 128

// MaxAddressees bounds how many entries Hub.Publish's to may carry, and
// MaxAddresseesBytes bounds their combined raw length (summed across every
// entry, not per entry — a large count of short names and a small count of
// long ones are both covered by one budget). Both are enforced by
// Hub.Publish and feed roomlog.go's scannerMaxLineBytes the same way
// MaxTextBytes and MaxIdentifierBytes do.
const (
	MaxAddressees      = 16
	MaxAddresseesBytes = 256
)

// Envelope is one message on a room: the unit sent to subscribers of recv
// --follow, tail, and chat, and the unit appended to a room's log.
type Envelope struct {
	ID       string `json:"id"`
	Room     string `json:"room"`
	From     string `json:"from"`
	FromKind string `json:"from_kind"`

	// FromRepo and FromRealm are the sender's position at join time
	// stamped server-side from the roster entry Hub.Join recorded, the same
	// way From/FromKind are, and never read from a
	// send/say request. Omitted when empty: a name can be released on
	// leave and reclaimed by an unrelated session, so these keep a room
	// log's history unambiguous about which position actually sent a
	// given line.
	FromRepo  string `json:"from_repo,omitempty"`
	FromRealm string `json:"from_realm,omitempty"`

	// To is the addressee list. An envelope with no addressees is an FYI
	// message to the whole room, not an addressed one — that distinction
	// drives each recipient's reaction policy (see skills/atomic-bus). To
	// must therefore always marshal as [], never as null or an omitted
	// key, so "no addressees" (this field) can never be confused with
	// "field absent" on the wire. See MarshalJSON below.
	To []string `json:"to"`

	ReplyTo string `json:"reply_to,omitempty"`
	// Ts is the envelope's timestamp. Wire representation is Unix seconds
	// (an integer, e.g. "ts":1753900000), not Go's default RFC3339Nano —
	// see docs/design/atomic-bus.md's wire protocol table and MarshalJSON/
	// UnmarshalJSON below, which carry the conversion. Sub-second precision
	// does not survive a marshal/unmarshal round trip; nothing in the
	// protocol needs it (see docs/spec/atomic-bus.md's envelope-shape
	// success criterion).
	Ts   time.Time `json:"ts"`
	Text string    `json:"text"`

	// Truncated is nonzero only when Text was cut for the notification
	// cap; it holds the number of bytes cut. Log then points at the room
	// log where the full body is recoverable. See docs/design/
	// atomic-bus.md, "Ambiguity resolved in the contract".
	Truncated int    `json:"truncated,omitempty"`
	Log       string `json:"log,omitempty"`

	// Closing marks the terminal envelope Hub.Close publishes just before
	// dropping a room — the signal recv's reconnect loop (action.go's
	// recvDeliver) uses to end its stream cleanly instead of reconnecting
	// to a room the operator explicitly closed, which would otherwise be
	// indistinguishable from an ordinary dropped connection. Never set on any
	// other envelope, including Halt/Resume's control envelopes, which
	// also publish From: systemName — Closing, not the sender identity, is
	// what disambiguates "stop" from "reconnect".
	Closing bool `json:"closing,omitempty"`
}

// MarshalJSON enforces the To invariant documented on the field above (an
// empty or nil To always serializes as "to":[], never "to":null) and the Ts
// invariant documented on that field (Unix seconds, not RFC3339Nano).
// Overriding here — instead of requiring every constructor to remember —
// makes both invariants hold regardless of how an Envelope was built.
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	to := e.To
	if to == nil {
		to = []string{}
	}
	// The outer To/Ts fields shadow the ones promoted from alias (same JSON
	// name, shallower struct depth) — the standard way to override a
	// field's encoding without hand-rolling every other field.
	return json.Marshal(struct {
		alias
		To []string `json:"to"`
		Ts int64    `json:"ts"`
	}{alias: alias(e), To: to, Ts: e.Ts.Unix()})
}

// UnmarshalJSON is MarshalJSON's inverse for Ts: it decodes the wire's Unix
// seconds integer back into a time.Time, so every Envelope consumer (room
// log round trips, subscription frames, response payloads) sees the same
// Go type regardless of which side of the wire it's on.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	type alias Envelope
	aux := struct {
		*alias
		Ts int64 `json:"ts"`
	}{alias: (*alias)(e)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	e.Ts = time.Unix(aux.Ts, 0)
	return nil
}

// KindAgent and KindHuman are the only two values Member.Kind (and a join
// request's Kind) may hold — Hub.Join rejects anything else with
// ExitUsage. Closing Kind to exactly these two values is what lets
// room.go's systemName reservation guarantee a real member's envelope can
// never be mistaken for a daemon control envelope (see room.go's validKind
// and Hub.Join).
const (
	KindAgent = "agent"
	KindHuman = "human"
)

// Member is one room participant, as reported by `who` and carried on every
// Envelope's implied roster context.
type Member struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // KindAgent or KindHuman
	Mode    string    `json:"mode,omitempty"`
	Session string    `json:"session"`
	Joined  time.Time `json:"joined"`

	// LastSeen is refreshed on any operation this Session performs against
	// the room (Join, Publish) — the "recent activity" half of staleness.
	LastSeen time.Time `json:"last_seen"`

	// Stale is computed only by Hub.Who (Room.isStale) — never stored, never
	// meaningful on a Member returned by Join or read from Rehydrate's
	// source state. True means neither LastSeen nor a live subscription
	// proves this member is still around; `who` surfaces it, `prune`
	// removes only members for which it holds. Not omitempty: "stale":false
	// is itself a signal a --json caller should be able to rely on, exactly
	// like Response.OK.
	Stale bool `json:"stale"`

	// Repo and Realm are the joining client's own position, resolved once
	// at join and never revised afterward. Repo is the
	// repo-root basename; Realm is the realm-root basename, empty when the
	// session was not inside a registered realm at join time — empty is
	// valid and common, never fabricated.
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`
}

// RoomInfo is one room's summary, as reported by `rooms`: its name, how many
// members currently hold it, and its halt state.
type RoomInfo struct {
	Name       string `json:"name"`
	Members    int    `json:"members"`
	Halted     bool   `json:"halted,omitempty"`
	HaltReason string `json:"halt_reason,omitempty"`
}

// ExitCode is a process exit status the bus CLI terminates with. Values are
// stable across the package: the daemon sets Response.Code from these, and
// client-side errors resolved before ever reaching the daemon (e.g.
// State.ResolveRoom's not-joined case) use the same values, so a caller
// checks one set of numbers regardless of where the error originated.
type ExitCode int

const (
	ExitOK ExitCode = iota
	ExitUsage
	ExitHard
	ExitNotJoined
	ExitNameTaken
	ExitNoRoom
	ExitUnreachable
	ExitHalted
)

// Error is a bus error carrying the exit code its caller should terminate
// with. It normalizes client-side errors (resolved locally, before a daemon
// round trip) to the same shape as a failed Response, so every call site in
// action.go checks one error type rather than re-deriving an exit code from
// message text.
type Error struct {
	Code ExitCode
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
