// Package bus implements atomic bus: a per-user daemon behind a Unix domain
// socket that lets concurrent Claude Code sessions on one machine message each
// other over named rooms. Contract: docs/spec/atomic-bus.md.
package bus

import (
	"encoding/json"
	"time"
)

// ProtocolVersion gates the client/daemon handshake: a client whose version
// differs from the running daemon's refuses to proceed rather than risk a wire
// format the other side does not understand. Bump it whenever the wire shape
// changes — TestProtocolWireShape_GoldenFieldsAndOps is what makes forgetting
// fail a test instead of shipping silently.
const ProtocolVersion = 3

// Op names. AllOps below is the same list as a slice; keep both in sync —
// TestProtocolWireShape_GoldenFieldsAndOps pins AllOps against a golden list.
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
	OpEnd      = "end"
)

// AllOps lists every Request.Op the daemon accepts. daemon.go's "unknown op"
// error enumerates it, so this is production content, not a test fixture.
var AllOps = []string{
	OpPing, OpJoin, OpLeave, OpSend, OpSay, OpRecv, OpTail, OpWho, OpRooms,
	OpHalt, OpResume, OpShutdown, OpPrune, OpClose, OpEnd,
}

// Request is a single client-to-daemon frame: an op plus whichever operand
// fields that op uses. One struct covers every op rather than one type per op,
// since the daemon's op switch already discriminates which fields apply.
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

	// Repo and Realm are the joining client's own position, reported once at
	// join like Mode and Kind — the daemon has no cwd to resolve them against.
	// They carry no weight on any other op: an envelope's from_repo/from_realm
	// are always stamped from the roster entry, never from the wire.
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`

	// SkipSelf opts an OpRecv subscription out of its own Session's envelopes —
	// set by `recv`, not by `tail`/`chat`, which want the whole transcript
	// including their own lines. Meaningless without Session, and ignored for
	// OpTail, which carries no identity to skip.
	SkipSelf bool `json:"skip_self,omitempty"`

	// Filters narrows a tail subscription (e.g. "only_addressed", "from"). A
	// generic map so a consumer can add keys without amending this file.
	Filters map[string]string `json:"filters,omitempty"`
}

// Response answers a Request, or opens a subscription before Envelope frames
// start arriving.
type Response struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	// Code is the exit code the client should terminate with on failure. The
	// mapping lives in the daemon that produces the response rather than being
	// re-derived from Error's text — one place decides what a failure means.
	Code ExitCode `json:"code,omitempty"`

	// Payload carries whatever reply shape the op produced. Generic here so this
	// file needs no struct per op; the daemon and client share the concrete
	// shapes at their encode/decode sites.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// MaxTextBytes bounds Envelope.Text (enforced by Hub.Publish) so a room log
// line can never grow unbounded.
const MaxTextBytes = 1024 * 1024

// MaxIdentifierBytes bounds Room, a member's assigned Name, and ReplyTo — the
// string metadata roomlog.go's scanner budget must hold alongside Text. See
// roomlog.go's scannerMaxLineBytes.
const MaxIdentifierBytes = 128

// MaxAddressees bounds how many entries Hub.Publish's to may carry, and
// MaxAddresseesBytes their combined length summed across entries, not per
// entry. Both feed roomlog.go's scannerMaxLineBytes.
const (
	MaxAddressees      = 16
	MaxAddresseesBytes = 256
)

// Envelope is one message on a room: the unit delivered to subscribers and the
// unit appended to a room's log.
type Envelope struct {
	ID       string `json:"id"`
	Room     string `json:"room"`
	From     string `json:"from"`
	FromKind string `json:"from_kind"`

	// FromRepo and FromRealm are stamped server-side from the roster entry, the
	// same way From/FromKind are, and never read from a request. A name can be
	// released on leave and reclaimed by an unrelated session, so these keep a
	// log's history unambiguous about which position actually sent a line.
	FromRepo  string `json:"from_repo,omitempty"`
	FromRealm string `json:"from_realm,omitempty"`

	// To is the addressee list. No addressees means an FYI to the whole room, not
	// an addressed message — the distinction that drives each recipient's
	// reaction policy. It must therefore always marshal as [], never null or an
	// omitted key, so "no addressees" can never read as "field absent".
	To []string `json:"to"`

	ReplyTo string `json:"reply_to,omitempty"`
	// Ts marshals as Unix seconds, not Go's default RFC3339Nano — see
	// MarshalJSON/UnmarshalJSON below. Sub-second precision does not survive the
	// round trip; nothing in the protocol needs it.
	Ts   time.Time `json:"ts"`
	Text string    `json:"text"`

	// Truncated is nonzero only when Text was cut for the notification cap, and
	// holds the bytes cut; Log then points at the room log holding the full body.
	Truncated int    `json:"truncated,omitempty"`
	Log       string `json:"log,omitempty"`

	// Closing marks the terminal envelope Hub.Close publishes before dropping a
	// room — what recv's reconnect loop uses to end cleanly instead of
	// reconnecting to a room the operator closed, which would otherwise look like
	// an ordinary dropped connection. Never set on any other envelope, including
	// Halt/Resume's, which share From: systemName — Closing, not the sender, is
	// what separates "stop" from "reconnect".
	Closing bool `json:"closing,omitempty"`
}

// MarshalJSON enforces the To and Ts invariants documented on those fields.
// Overriding here rather than asking every constructor to remember makes both
// hold regardless of how an Envelope was built.
func (e Envelope) MarshalJSON() ([]byte, error) {
	type alias Envelope
	to := e.To
	if to == nil {
		to = []string{}
	}
	// The outer To/Ts fields shadow the ones promoted from alias (same JSON name,
	// shallower depth) — the standard way to override two fields' encoding
	// without hand-rolling every other one.
	return json.Marshal(struct {
		alias
		To []string `json:"to"`
		Ts int64    `json:"ts"`
	}{alias: alias(e), To: to, Ts: e.Ts.Unix()})
}

// UnmarshalJSON is MarshalJSON's inverse for Ts, so every Envelope consumer
// sees a time.Time regardless of which side of the wire it is on.
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

// KindAgent and KindHuman are the only values Member.Kind may hold; Hub.Join
// rejects anything else. Closing Kind to these two is what lets room.go's
// systemName reservation guarantee a member's envelope can never be mistaken
// for a daemon control envelope.
const (
	KindAgent = "agent"
	KindHuman = "human"
)

// Member is one room participant, as reported by `who`.
type Member struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // KindAgent or KindHuman
	Mode    string    `json:"mode,omitempty"`
	Session string    `json:"session"`
	Joined  time.Time `json:"joined"`

	// LastSeen is refreshed on any operation this Session performs against the
	// room — the "recent activity" half of staleness.
	LastSeen time.Time `json:"last_seen"`

	// Stale is computed only by Hub.Who, never stored, and meaningless on a
	// Member returned by Join or read from Rehydrate's source. Not omitempty:
	// "stale":false is itself a signal a --json caller relies on.
	Stale bool `json:"stale"`

	// Repo and Realm are resolved once at join and never revised. Repo is the
	// repo-root basename; Realm is the realm-root basename, empty when the
	// session was not inside a registered realm — valid, common, never fabricated.
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`
}

// RoomInfo is one room's summary, as reported by `rooms`.
type RoomInfo struct {
	Name       string `json:"name"`
	Members    int    `json:"members"`
	Halted     bool   `json:"halted,omitempty"`
	HaltReason string `json:"halt_reason,omitempty"`
}

// ExitCode is a process exit status the bus CLI terminates with. The daemon
// sets Response.Code from these and client-side errors use the same values, so
// a caller checks one set of numbers wherever the error originated.
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

// Error is a bus error carrying the exit code its caller should terminate with.
// It normalizes a locally-resolved error to the same shape as a failed
// Response, so every call site checks one error type.
type Error struct {
	Code ExitCode
	Msg  string
}

func (e *Error) Error() string { return e.Msg }
