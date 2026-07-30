package bus

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/ids"
)

// subscriberBuffer bounds each live subscriber's delivery channel. See
// Room.fanOut for why a full channel drops rather than blocks.
const subscriberBuffer = 32

// Hub owns every room a running daemon knows about, behind one mutex. A
// single mutex — rather than one per room — is deliberate: "never allow
// two backends in one room" is a compare-and-swap against the roster, and
// that check-then-set can only be atomic if nothing else can observe or
// mutate room state in between. See docs/design/atomic-bus.md's
// "Approach A" rationale and room_test.go's concurrent-join tests, which
// are the actual proof of this property.
//
// The same mutex also serializes every room's disk I/O: Publish and
// setHalted append to the room log synchronously while h.mu is held, so
// one room's slow write blocks every other room's operation until it
// returns. That is an acceptable trade at this daemon's scale (one
// process, one user, local disk) — it is a consequence of choosing
// roster atomicity over per-room locks, not a design goal, and it is
// worth knowing before assuming Publish is cheap.
type Hub struct {
	home string
	now  func() time.Time

	mu    sync.Mutex
	rooms map[string]*Room
}

// NewHub creates a Hub whose room logs are written under home (see
// RoomLogPath in paths.go). Its clock defaults to time.Now; see SetClock.
func NewHub(home string) *Hub {
	return &Hub{home: home, now: time.Now, rooms: map[string]*Room{}}
}

// SetClock overrides Hub's time source — the seam staleness tests use to
// advance "now" without a real sleep. Production code (serveAction) never
// calls this; NewHub's time.Now default is what every real daemon runs on.
// Not safe to call once the Hub is serving concurrent requests — set it
// once, immediately after NewHub, before any goroutine can observe h.mu.
func (h *Hub) SetClock(now func() time.Time) {
	h.now = now
}

// Room is one named room's authoritative state: who's in it, whether it's
// halted, and who is currently subscribed to its live traffic.
//
// Room has no lock of its own — every field here is guarded by the owning
// Hub's mutex, and every method on Room assumes that lock is already held.
// A Room with its own lock would let a caller correctly serialize its own
// calls while still racing another goroutine going through Hub directly;
// one lock for all room state removes that whole class of mistake.
type Room struct {
	members   map[string]Member // by assigned name
	bySession map[string]string // session id -> assigned name

	halted bool
	// haltReason is the text a Halt call was given, cleared on Resume.
	// Retained (not just broadcast at halt time) so rooms/who/status can
	// report why a room is halted at any later point, including after a
	// daemon restart rehydrates it from persisted state (see Rehydrate below;
	// docs/spec/atomic-bus.md's 2026-07-30 "halt must persist and be
	// visible" entry).
	haltReason string

	// usedIDs records every envelope id this Room has assigned during this
	// daemon's lifetime — nextEnvelopeID's collision guard. See that
	// method's doc for why a per-process sequential counter (the prior
	// design) was replaced: it made ids unique only within one daemon's
	// lifetime, and the room log they land in outlives the daemon.
	usedIDs map[string]struct{}

	subs   map[int]*subscriber
	subSeq int
}

// subscriber pairs a live subscriber's delivery channel with its own drop
// count, the session it was opened for, and whether it opts out of
// receiving that session's own publishes. dropped is only ever touched from
// fanOut, which always runs under the owning Hub's mutex (see Room's doc
// comment) — no separate lock needed.
//
// session and skipSelf are also Room.hasLiveSubscription's and fanOut's
// only source of "is this session currently watching" — the plumbing item 2
// (self-echo) and item 3 (liveness) share, per docs/spec/atomic-bus.md's
// 2026-07-29 change-log entry: "Hub.Subscribe(room, ch) carries no
// identity, and fanOut iterates every subscriber, so the daemon cannot
// currently tell who published."
type subscriber struct {
	ch       chan<- Envelope
	dropped  int
	session  string
	skipSelf bool
}

// getOrCreateRoom returns the named room, creating it if this is the first
// time anything (a join or a subscribe) has touched it. Caller must hold
// h.mu.
func (h *Hub) getOrCreateRoom(name string) *Room {
	r, ok := h.rooms[name]
	if !ok {
		r = &Room{
			members:   map[string]Member{},
			bySession: map[string]string{},
			usedIDs:   map[string]struct{}{},
			subs:      map[int]*subscriber{},
		}
		h.rooms[name] = r
	}
	return r
}

// getRoom returns the named room without creating it. Caller must hold h.mu.
func (h *Hub) getRoom(name string) (*Room, bool) {
	r, ok := h.rooms[name]
	return r, ok
}

func noRoomError(room string) error {
	return &Error{Code: ExitNoRoom, Msg: fmt.Sprintf("bus: room %q does not exist", room)}
}

// systemName is the sentinel identity daemon-generated control envelopes
// use as From — setHalted's halt/resume announcement and fanOut's drop
// marker (dropMarkerEnvelope). Join rejects any real member claiming this
// name (see the check below), so From == systemName is proof a subscriber
// can trust: no member can ever publish under this name, only the daemon's
// own sentinel envelopes carry it.
//
// kindSystem is dropMarkerEnvelope's FromKind, deliberately outside
// validKind's {KindAgent, KindHuman} enum — Join can never assign it to a
// member, so FromKind == kindSystem is exactly as unspoofable as
// From == systemName. setHalted's control envelope uses KindHuman instead
// (see docs/design/atomic-bus.md's halt flow, step 2); systemName alone is
// what makes that one unspoofable too.
// operatorName is the fixed From of every `say` / `halt` / `resume` envelope.
// The daemon assigns it in handleSay — it is never read from the request — and
// Join reserves it exactly as it reserves systemName, so From == operatorName
// is proof the message came from a human operator. That proof is load-bearing:
// the skill tells agents to treat operator messages as authoritative user
// input, so a forgeable operator identity is a privilege escalation between
// agents, not a cosmetic confusion.
const (
	systemName   = "system"
	kindSystem   = "system"
	operatorName = "human"
)

// reservedNames are the sentinel From values no member may claim at Join.
// Adding a sentinel elsewhere in the package means adding it here — that is
// the point of the set existing rather than a chain of != comparisons.
var reservedNames = map[string]bool{
	systemName:   true,
	operatorName: true,
}

// validKind reports whether kind is one of the two values Member.Kind
// accepts (protocol.go's KindAgent/KindHuman). Join rejects anything else
// with ExitUsage — a client-supplied Kind is wire input, so an unknown
// value must be a clean protocol error, never silently stored or panicked
// on. Closing Kind to exactly these two values is also what makes
// Publish's halt check load-bearing in the form it's written: see that
// check's doc comment.
func validKind(kind string) bool {
	return kind == KindAgent || kind == KindHuman
}

// Join claims name in room, atomically: taken -> retry once as
// "<name>-2" -> still taken -> ExitNameTaken. The whole operation runs
// under h.mu as a single critical section, so the check ("is this name
// free?") and the claim ("take it") can never be separated by another
// goroutine's Join landing in between — that gap is exactly what would let
// two "backend"s exist in one room. See room_test.go's concurrent-join
// tests for the actual proof.
//
// Nothing about session's existing roster entry is touched until the new
// name is confirmed claimable — a failed Join (ExitNameTaken) is a no-op
// on the roster, full stop. Only once assigned is known free does Join
// release session's prior entry (if any) and take assigned; a session
// re-joining a room it's already in can therefore never end up holding two
// roster entries, and a session whose rejoin fails can never end up
// holding zero.
//
// Join also validates before any of that: room and name are capped at
// MaxIdentifierBytes (see roomlog.go's scannerMaxLineBytes, which depends
// on this cap holding), kind must be one of validKind's two values, and
// name may not be systemName — closing off the two ways a member could
// otherwise spoof a daemon control envelope (see systemName's doc
// comment).
//
// repo and realm are the joining client's own reported position
// (docs/spec/atomic-bus.md's 2026-07-29 "position-derived member naming"
// entry) — stored on the roster verbatim, same trust level as mode: unlike
// From/FromKind at publish time, there is no roster entry yet to check
// these against, since this call is what creates one. Empty realm is valid
// and common; never rewritten to a placeholder.
func (h *Hub) Join(room, name, mode, kind, session, repo, realm string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(room) > MaxIdentifierBytes {
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: room name is %d bytes, over the %d-byte limit (MaxIdentifierBytes)", len(room), MaxIdentifierBytes),
		}
	}
	if len(name) > MaxIdentifierBytes {
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: name is %d bytes, over the %d-byte limit (MaxIdentifierBytes)", len(name), MaxIdentifierBytes),
		}
	}
	if reservedNames[name] {
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: name %q is reserved for daemon and operator envelopes", name),
		}
	}
	if !validKind(kind) {
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: kind %q is invalid; must be %q or %q", kind, KindAgent, KindHuman),
		}
	}

	r := h.getOrCreateRoom(room)

	assigned := name
	if !r.nameAvailableTo(assigned, session) {
		assigned = name + "-2"
		if !r.nameAvailableTo(assigned, session) {
			return "", &Error{
				Code: ExitNameTaken,
				Msg:  fmt.Sprintf("bus: name %q (and %q) already taken in room %q", name, assigned, room),
			}
		}
	}

	if prior, ok := r.bySession[session]; ok && prior != assigned {
		delete(r.members, prior)
	}

	now := h.now()
	r.members[assigned] = Member{Name: assigned, Kind: kind, Mode: mode, Session: session, Joined: now, LastSeen: now, Repo: repo, Realm: realm}
	r.bySession[session] = assigned
	return assigned, nil
}

// nameAvailableTo reports whether candidate can be claimed by session: free
// outright, or already held by session itself (so a session re-asserting
// its own current name is never blocked by its own entry).
func (r *Room) nameAvailableTo(candidate, session string) bool {
	m, taken := r.members[candidate]
	return !taken || m.Session == session
}

// Rehydrate restores every room and member recorded in st into the Hub —
// the startup step that rebuilds the whole roster from ~/.atomic/bus.json
// (docs/spec/atomic-bus.md: "a restarted daemon rehydrates the whole
// roster ... not one session at a time as each happens to run a command").
// bus.json already holds every session on the machine, not just whichever
// one's next command happens to notice the daemon is gone, so this one
// pass at Serve startup (daemon.go) is what makes a member who stays idle
// across the restart still present and addressable — the per-client
// re-registration this replaced could only ever restore a session that ran
// a command.
//
// Rehydrate bypasses Join's name-collision retry entirely: a restored
// member owns its name by right — it is the authoritative record of who
// already held that name before the daemon went away — not a new claim
// racing whatever else is in the (freshly empty) roster. Kind and Mode
// default to KindAgent and "participate" — Join's own defaults — for a
// membership persisted before those fields existed, so an old bus.json
// entry rehydrates exactly as a fresh join would have assigned it.
func (h *Hub) Rehydrate(st *State) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for session, ss := range st.Sessions {
		for room, m := range ss.Rooms {
			kind := m.Kind
			if kind == "" {
				kind = KindAgent
			}
			mode := m.Mode
			if mode == "" {
				mode = "participate"
			}
			// A rehydrated member's LastSeen is restored from what was
			// persisted, not restamped to "now" — restamping is exactly the
			// bug this fixes: it resurrected a session dead for hours as
			// freshly live and put it permanently out of Prune's reach
			// (docs/spec/atomic-bus.md's 2026-07-30 "last_seen must persist,
			// not be restamped" entry). A zero LastSeen means this entry was
			// written before the field existed; Joined is the best available
			// signal of that member's last known activity, and (unlike
			// LastSeen on an old entry) is never zero.
			lastSeen := m.LastSeen
			if lastSeen.IsZero() {
				lastSeen = m.Joined
			}
			r := h.getOrCreateRoom(room)
			r.members[m.Name] = Member{Name: m.Name, Kind: kind, Mode: mode, Session: session, Joined: m.Joined, LastSeen: lastSeen, Repo: m.Repo, Realm: m.Realm}
			r.bySession[session] = m.Name
		}
	}

	// Halt is room-level, not tied to any one session's membership — restore
	// it independently so a room an operator halted comes back halted even
	// if nobody currently occupies it (docs/spec/atomic-bus.md: "halt
	// survives a daemon restart").
	for room, rs := range st.Rooms {
		if rs == nil || !rs.Halted {
			continue
		}
		r := h.getOrCreateRoom(room)
		r.halted = true
		r.haltReason = rs.HaltText
	}
}

// UnknownAddressees reports which entries of to are not currently members
// of room — send --to <name> uses this to warn on an addressed message
// that reaches nobody (docs/spec/atomic-bus.md: "send --to <name> warns on
// stderr when no such member is in the room"). This never blocks or alters
// delivery: a named member may legitimately be about to join, so Publish
// still sends unconditionally (see docs/spec/atomic-bus.md's Finding 3
// change-log entry) — this is only the signal a caller uses to warn.
func (h *Hub) UnknownAddressees(room string, to []string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return to
	}
	var unknown []string
	for _, name := range to {
		if _, present := r.members[name]; !present {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

// Leave removes session's membership from room, then drops the room
// entirely when that was its last member and nothing is subscribed to it
// (dropIfEmpty) — reported back as dropped so callers with room-scoped
// persisted state (e.g. a halt flag) know to clear it too
// (docs/spec/atomic-bus.md's 2026-07-30 "drop a room when its last member
// leaves" entry).
func (h *Hub) Leave(room, session string) (dropped bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return false, noRoomError(room)
	}
	name, ok := r.bySession[session]
	if !ok {
		return false, &Error{Code: ExitNotJoined, Msg: fmt.Sprintf("bus: session has not joined room %q", room)}
	}
	delete(r.members, name)
	delete(r.bySession, session)

	return h.dropIfEmpty(room, r), nil
}

// dropIfEmpty removes room from the Hub when it has no members and no live
// subscribers — a room created by a typo, or simply finished with, does not
// outlive the mistake (docs/spec/atomic-bus.md: "a room disappears when its
// last member leaves"). The subscriber check is what keeps this from
// yanking a room out from under a live `tail` or `recv`: those hold no
// roster membership, so a room with subscribers but zero members must stay
// — dropping it here would orphan them, since any future Publish to this
// room name would create a brand new Room object with an empty subs map,
// never reaching them again. Caller must hold h.mu.
func (h *Hub) dropIfEmpty(room string, r *Room) bool {
	if len(r.members) > 0 || len(r.subs) > 0 {
		return false
	}
	delete(h.rooms, room)
	return true
}

// Who returns room's current roster, sorted by name for stable output. Each
// returned Member's Stale field is computed fresh against the current clock
// (Room.isStale) — Stale is never persisted, only reported at query time.
func (h *Hub) Who(room string) ([]Member, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return nil, noRoomError(room)
	}
	now := h.now()
	out := make([]Member, 0, len(r.members))
	for _, m := range r.members {
		m.Stale = r.isStale(m, now)
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// staleThreshold is how long a member may go with neither fresh LastSeen
// activity nor a live subscription before who/prune consider it stale.
// Chosen to match the idle-shutdown default this package used to run on
// before that mechanism was removed entirely (docs/spec/atomic-bus.md's
// 2026-07-28 "idle shutdown removed" entry) — ten minutes already proved
// itself a reasonable "this session is gone" bar for one Claude Code agent
// turn (think, tool calls, reply) without being trigger-happy on an agent
// mid-task. A member holding an open recv/chat subscription is never stale
// regardless of this threshold — see isStale below — so staleThreshold only
// bites a member that joined and then neither sent anything nor kept a
// subscription open, e.g. a `join` with no following `Monitor(recv)`.
//
// Whatever value is picked here is a judgment call, not a derived
// constant — there is no wire contract or external system dictating it,
// only "long enough that a normal quiet spell never gets flagged, short
// enough that `who` is still a useful signal". Named and isolated here so
// it can be revisited without touching the staleness logic itself.
const staleThreshold = 10 * time.Minute

// isStale reports whether m should currently be treated as gone: no recent
// activity (LastSeen within staleThreshold of now) and no live subscription
// for its session (hasLiveSubscription). A live subscription overrides
// LastSeen entirely — a member that's connected and just hasn't sent
// anything is not stale no matter how long that's been, because the
// subscription itself is ongoing proof of life (docs/spec/atomic-bus.md:
// "refreshed on any operation from that session and on an open
// subscription"). Caller must hold h.mu (reads r.subs via
// hasLiveSubscription).
func (r *Room) isStale(m Member, now time.Time) bool {
	if now.Sub(m.LastSeen) <= staleThreshold {
		return false
	}
	return !r.hasLiveSubscription(m.Session)
}

// hasLiveSubscription reports whether any currently-open subscription in
// this room belongs to session. An empty session (operator publishes,
// tail's subscriptions — see Subscribe's callers) never counts: it cannot
// be any member's session, since Hub.Join always assigns one.
func (r *Room) hasLiveSubscription(session string) bool {
	if session == "" {
		return false
	}
	for _, sub := range r.subs {
		if sub.session == session {
			return true
		}
	}
	return false
}

// Prune removes every member of room currently marked stale (isStale) and
// reports their names, sorted. This is the one place in the package that
// removes a member without that session asking to leave — deliberately
// explicit and operator-invoked, never automatic: docs/spec/atomic-bus.md
// is direct about why — "nothing reaps a member silently ... a quiet
// session is not a dead one, and evicting a live member would break
// addressing with no diagnostic."
func (h *Hub) Prune(room string) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return nil, noRoomError(room)
	}

	now := h.now()
	var removed []string
	for name, m := range r.members {
		if r.isStale(m, now) {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	for _, name := range removed {
		m := r.members[name]
		delete(r.members, name)
		delete(r.bySession, m.Session)
	}
	return removed, nil
}

// Rooms returns a summary of every room the Hub currently knows about
// (created by a join or a subscribe): name plus current member count,
// sorted by name. A room that has emptied because every joined member left
// is still reported, with Members == 0 — see room_test.go's
// TestHub_Rooms_ListsEveryKnownRoomSorted for the leave-then-list case.
func (h *Hub) Rooms() []RoomInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]RoomInfo, 0, len(h.rooms))
	for name, r := range h.rooms {
		out = append(out, RoomInfo{Name: name, Members: len(r.members), Halted: r.halted, HaltReason: r.haltReason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Publish assigns an id and timestamp, stamps from/from_kind/from_repo/
// from_realm from the sender's roster membership — never from the request,
// the same invariant that governs from/from_kind (docs/spec/atomic-bus.md's
// 2026-07-29 "position-derived member naming" entry) — appends
// unconditionally to the durable room log, and fans out to live
// subscribers.
//
// Halt is enforced here, not merely advertised: a member whose kind is not
// exactly KindHuman is rejected before any of that happens when the room
// is halted. See docs/design/atomic-bus.md, "Resolved open decisions" #4 —
// halt only makes unattended agent-to-agent loops safe if the daemon
// itself refuses the send; an advisory flag is something the looping agent
// that most needs stopping is exactly the one that would ignore.
//
// Written as "!= KindHuman" rather than "== KindAgent" deliberately: Kind
// is closed to exactly {KindAgent, KindHuman} by Join's validKind check, so
// the two forms are equivalent today — but "!= KindHuman" is the
// load-bearing choice, kept as the safer fail-closed default should a
// third kind ever be added (an unrecognized future kind gets blocked, not
// waved through).
func (h *Hub) Publish(room, session string, to []string, replyTo, text string) (Envelope, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return Envelope{}, noRoomError(room)
	}
	name, ok := r.bySession[session]
	if !ok {
		return Envelope{}, &Error{Code: ExitNotJoined, Msg: fmt.Sprintf("bus: session has not joined room %q", room)}
	}
	member := r.members[name]

	if r.halted && member.Kind != KindHuman {
		return Envelope{}, &Error{
			Code: ExitHalted,
			Msg:  fmt.Sprintf("bus: room %q is halted; a human must resume it before agents can send", room),
		}
	}

	// A successful send is "an operation from that session" — refresh
	// LastSeen before publishing (docs/spec/atomic-bus.md's last_seen
	// criterion). member is a map value, not a pointer, so the touched copy
	// must be written back.
	now := h.now()
	member.LastSeen = now
	r.members[name] = member

	resolvedTo, err := r.resolveAddressees(to)
	if err != nil {
		return Envelope{}, err
	}
	return r.publishValidated(h.home, room, name, member.Kind, member.Repo, member.Realm, resolvedTo, replyTo, text, session, now)
}

// PublishAs publishes on behalf of name/kind directly, without requiring
// name to hold a room membership via Join — the path `say` uses to speak
// into a room without occupying a roster slot or appearing in `who`
// (docs/spec/atomic-bus.md checkpoint 5: "say — one-shot send without
// joining"). Unlike Publish, room must already exist (getRoom, not
// getOrCreateRoom) — nothing is listening in a room nobody has ever
// joined, mirroring Halt/Resume's own "room must exist" contract.
//
// The sender identity is fixed — operatorName / KindHuman — and deliberately
// not a parameter. An earlier signature took name and kind from the caller and
// was reachable from the wire via OpSay, which let any local process publish
// under an existing agent's name with kind "agent" and, because this path does
// not consult the halt flag, speak into a halted room. Both the impersonation
// and the halt bypass came from the daemon trusting a client-supplied identity
// — the same mistake Join's reserved-name and kind-enum checks exist to
// prevent. A function that cannot accept an identity cannot be talked into
// believing one.
//
// Skipping the halt check is correct here precisely because the identity is
// pinned: halt binds agents, and a human is the one who lifts it. Publish's own
// check lets KindHuman through for the same reason.
func (h *Hub) PublishAsOperator(room string, to []string, replyTo, text string) (Envelope, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return Envelope{}, noRoomError(room)
	}
	resolvedTo, err := r.resolveAddressees(to)
	if err != nil {
		return Envelope{}, err
	}
	// "" for publisherSession: an operator publish is never tied to a
	// joined session's subscription, so it can never match (and therefore
	// never wrongly self-skip) any subscriber's skipSelf check in fanOut —
	// see that method's doc. "", "" for repo/realm: the operator is not a
	// roster member with a resolved position — say never joins, so there is
	// no Member to read one from.
	return r.publishValidated(h.home, room, operatorName, KindHuman, "", "", resolvedTo, replyTo, text, "", h.now())
}

// resolveAddressees resolves every entry of to against r's current
// membership: an entry naming an existing member verbatim passes through
// unchanged (see resolveOneAddressee), a suffix/substring match resolving
// to more than one member aborts the whole send (never a partial publish
// under a half-resolved to list), and any other entry passes through
// untouched for Hub.UnknownAddressees's own "not currently in room"
// warning to catch. Caller must hold h.mu (resolveOneAddressee reads
// r.members).
func (r *Room) resolveAddressees(to []string) ([]string, error) {
	if len(to) == 0 {
		return to, nil
	}
	resolved := make([]string, len(to))
	for i, name := range to {
		match, err := r.resolveOneAddressee(name)
		if err != nil {
			return nil, err
		}
		resolved[i] = match
	}
	return resolved, nil
}

// resolveOneAddressee resolves one --to entry against r's roster
// (docs/spec/atomic-bus.md's 2026-07-29 "the name is the position; --as is
// the role" entry: "--to resolves an exact name first, then a unique
// suffix or substring"). A fully stacked name is long to type correctly by
// hand, so this is what lets "--to fe-main" reach
// "taxgentic-gui-fe-main" without the sender typing the whole thing.
//
// Exact match wins outright, before any scan — the case that matters once
// a "-2" collision sibling exists: "--to taxgentic-gui-fe-main" must reach
// exactly that member, never a longer name that happens to contain it as a
// substring too. Short of an exact hit, strings.Contains covers both
// "suffix" and "substring" in one pass (a suffix is a substring that
// happens to end the string) — a unique match resolves to that member's
// name; more than one match is an ambiguous --to, and the failure this
// whole resolution scheme exists to avoid is a silent pick among them, so
// this returns an error naming every candidate instead. Zero matches is
// deliberately not an error here: it passes name through unresolved, the
// same as before this resolution step existed, so Hub.UnknownAddressees's
// existing "not currently in room" warning — a softer failure that still
// delivers — covers a genuine typo or a peer about to join, and this
// stricter ambiguity error is reserved for the case where the sender's
// intent is genuinely unclear rather than simply wrong. Caller must hold
// h.mu.
func (r *Room) resolveOneAddressee(name string) (string, error) {
	if _, ok := r.members[name]; ok {
		return name, nil
	}
	var candidates []string
	for member := range r.members {
		if strings.Contains(member, name) {
			candidates = append(candidates, member)
		}
	}
	switch len(candidates) {
	case 0:
		return name, nil
	case 1:
		return candidates[0], nil
	default:
		sort.Strings(candidates)
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: --to %q is ambiguous among %s", name, strings.Join(candidates, ", ")),
		}
	}
}

// publishValidated is the shared tail end of Publish and PublishAs: once
// the caller has resolved from/fromKind/fromRepo/fromRealm (via a roster
// lookup, or supplied directly) and cleared any halt check, this validates
// the wire-size limits (MaxTextBytes, MaxIdentifierBytes for replyTo,
// MaxAddressees/MaxAddresseesBytes for to), assigns an id, appends to the
// durable room log, and fans out to subscribers. publisherSession is "" for
// PublishAsOperator's operator sends (see that method's doc) and the
// sending session id for Publish's member sends — fanOut's self-echo check
// against it. now is the single timestamp this call stamps onto the
// envelope and (via Publish) the sender's LastSeen, so both agree exactly.
// Caller must hold h.mu (both Hub.Publish and Hub.PublishAsOperator do).
func (r *Room) publishValidated(home, room, from, fromKind, fromRepo, fromRealm string, to []string, replyTo, text string, publisherSession string, now time.Time) (Envelope, error) {
	if len(text) > MaxTextBytes {
		return Envelope{}, &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: message is %d bytes, over the %d-byte limit (MaxTextBytes)", len(text), MaxTextBytes),
		}
	}
	if len(replyTo) > MaxIdentifierBytes {
		return Envelope{}, &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: reply_to is %d bytes, over the %d-byte limit (MaxIdentifierBytes)", len(replyTo), MaxIdentifierBytes),
		}
	}
	if len(to) > MaxAddressees {
		return Envelope{}, &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: %d addressees, over the %d-addressee limit (MaxAddressees)", len(to), MaxAddressees),
		}
	}
	addresseeBytes := 0
	for _, addr := range to {
		addresseeBytes += len(addr)
	}
	if addresseeBytes > MaxAddresseesBytes {
		return Envelope{}, &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: addressees total %d bytes, over the %d-byte limit (MaxAddresseesBytes)", addresseeBytes, MaxAddresseesBytes),
		}
	}

	id, err := r.nextEnvelopeID()
	if err != nil {
		return Envelope{}, err
	}
	env := Envelope{
		ID:        id,
		Room:      room,
		From:      from,
		FromKind:  fromKind,
		FromRepo:  fromRepo,
		FromRealm: fromRealm,
		To:        to,
		ReplyTo:   replyTo,
		Ts:        now,
		Text:      text,
	}

	if err := Append(home, room, env); err != nil {
		return Envelope{}, fmt.Errorf("bus: append room log: %w", err)
	}

	r.fanOut(env, home, publisherSession)
	return env, nil
}

// Halt sets room's halt flag and publishes a control envelope announcing
// it (docs/design/atomic-bus.md's halt flow, step 2). Halt does not
// require the caller to be a joined member — an operator can stop a room
// whether or not they are currently in it — so the control envelope's From
// is the fixed sentinel "system" rather than a roster name.
func (h *Hub) Halt(room, text string) error {
	return h.setHalted(room, true, text)
}

// Resume clears room's halt flag and publishes the clearing envelope. An
// empty text is replaced with defaultResumeText — see that constant's doc.
func (h *Hub) Resume(room, text string) error {
	return h.setHalted(room, false, text)
}

// defaultResumeText is the envelope body setHalted publishes when Resume is
// called with no explicit text — a resume notification must never carry an
// empty body (docs/spec/atomic-bus.md: "resume publishes an envelope with a
// body, not an empty string"). Halt is unaffected: an operator's empty
// --text on halt is left exactly as given, unchanged by this fix — an
// agent reading a halt with no reason still learns the one fact that
// matters (the room is halted), where an empty resume notification carries
// nothing to act on at all.
const defaultResumeText = "room resumed"

// setHalted only flips r.halted once the control envelope announcing it is
// durably appended — an Append failure returns an error to the operator,
// and that error must be true: if the flag flipped first and Append then
// failed, the room would in fact be halted with no control envelope ever
// logged or broadcast to prove it, while the operator's error implied the
// halt might not have taken effect at all.
func (h *Hub) setHalted(room string, halted bool, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return noRoomError(room)
	}

	id, err := r.nextEnvelopeID()
	if err != nil {
		return err
	}
	body := text
	if !halted && body == "" {
		body = defaultResumeText
	}
	env := Envelope{
		ID:       id,
		Room:     room,
		From:     systemName,
		FromKind: KindHuman,
		Ts:       h.now(),
		Text:     body,
	}
	if err := Append(h.home, room, env); err != nil {
		return fmt.Errorf("bus: append room log: %w", err)
	}

	r.halted = halted
	if halted {
		r.haltReason = text
	} else {
		r.haltReason = ""
	}
	// "" for publisherSession: a halt/resume control envelope is never a
	// member's own send, so it can never wrongly trip a subscriber's
	// skipSelf check — same reasoning as PublishAsOperator's own "" above.
	r.fanOut(env, h.home, "")
	return nil
}

// IsHalted reports whether room currently has its halt flag set, and the
// reason given at halt time (empty when not halted, or when halted with no
// --text) — the query handleWho/handleRooms use to surface halt state
// alongside a room's own contents (docs/spec/atomic-bus.md's 2026-07-30
// "halt must persist and be visible" entry).
func (h *Hub) IsHalted(room string) (halted bool, reason string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return false, "", noRoomError(room)
	}
	return r.halted, r.haltReason, nil
}

// Close publishes a final "room closed" envelope, evicts every member,
// ends every live subscriber's stream, and drops the room from the Hub
// entirely — an operator-level operation like Halt, needing no prior
// membership (docs/spec/atomic-bus.md: "close ... Operator-level, like
// halt/say/tail — no session identity required"). The room log on disk is
// never touched: it is the durable record, and a roster operation must not
// delete it.
//
// Closing a subscriber's channel (rather than merely unregistering it, as
// dropIfEmpty's guard exists to protect) is deliberate here: Close's whole
// point is that a listener learns why it stopped, not merely that it did.
// daemon.go's subscribe loop now checks the channel's ok value on every
// receive so this terminates the connection cleanly instead of spinning on
// a closed channel's zero-value reads.
func (h *Hub) Close(room string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return noRoomError(room)
	}

	id, err := r.nextEnvelopeID()
	if err != nil {
		return err
	}
	env := Envelope{
		ID:       id,
		Room:     room,
		From:     systemName,
		FromKind: KindHuman,
		Ts:       h.now(),
		Text:     "room closed",
		Closing:  true,
	}
	if err := Append(h.home, room, env); err != nil {
		return fmt.Errorf("bus: append room log: %w", err)
	}
	r.fanOut(env, h.home, "")

	for _, sub := range r.subs {
		close(sub.ch)
	}

	delete(h.rooms, room)
	return nil
}

// SessionIsMember reports whether session currently holds a membership in
// room — the check daemon.go's OpRecv dispatch uses to refuse a
// client-claimed session it does not actually own before handing it to
// Subscribe (see that dispatch's doc comment). There is no way to prove a
// connection genuinely *is* the session it names — the socket has no
// authentication beyond Unix file permissions — so this cannot close every
// spoofing path; what it does close is a session that names nobody, or not
// yet nobody: a subscription opened under a session before that session has
// joined the room can no longer sit in r.subs waiting to attach itself to
// whichever member happens to join under that name later and silently keep
// them non-stale from that moment on. An empty session or an unknown room
// both report false — there is nothing to validate against, and Subscribe's
// own contract already treats "" as "no session of its own" (tail's case).
func (h *Hub) SessionIsMember(room, session string) bool {
	if session == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return false
	}
	_, ok = r.bySession[session]
	return ok
}

// Subscribe registers ch to receive every future Publish (including
// Halt/Resume's control envelopes) on room, creating room if it doesn't
// exist yet — tail may watch a room before anyone has joined it (see
// docs/design/atomic-bus.md's decision #5: tail never joins and holds no
// name). session identifies the subscribing session for fanOut's self-echo
// check and hasLiveSubscription's liveness check — pass "" when the caller
// has no session of its own (tail's subscriptions; a caller that never
// sends and therefore has nothing to self-skip). Subscribe itself trusts
// session verbatim — it is OpRecv's dispatch (daemon.go), via
// SessionIsMember above, that is responsible for downgrading an unowned
// claim to "" before it ever reaches here; OpTail always passes "" directly,
// having no identity to skip in the first place. skipSelf, meaningful only
// when session is non-empty, opts this subscription out of receiving
// envelopes published by that same session (fanOut). The returned func
// removes the subscription; callers must invoke it exactly once, typically
// via defer, when the subscribing connection ends.
func (h *Hub) Subscribe(room string, ch chan<- Envelope, session string, skipSelf bool) func() {
	h.mu.Lock()
	r := h.getOrCreateRoom(room)
	id := r.subSeq
	r.subSeq++
	r.subs[id] = &subscriber{ch: ch, session: session, skipSelf: skipSelf}
	h.mu.Unlock()

	return func() {
		h.mu.Lock()
		delete(r.subs, id)
		h.mu.Unlock()
	}
}

// --- Room internals. All of the following assume h.mu is already held. ---

// messageIDPrefix names every opaque envelope id nextEnvelopeID assigns
// (e.g. "m-3f2ab71c") — short and opaque per docs/spec/atomic-bus.md's
// envelope-shape success criterion.
const messageIDPrefix = "m"

// maxIDGenAttempts bounds nextEnvelopeID's collision-retry loop — the same
// "generate, check, retry a few times" shape internal/reminder's Add uses
// for its own ids.ShortID-derived filenames.
const maxIDGenAttempts = 5

// nextEnvelopeID assigns a short opaque id, replacing the sequential
// per-process counter this used to be. A counter reset to zero on every
// daemon restart while the room log it writes into is durable and outlives
// the daemon: two different messages, from two different daemon lifetimes,
// would both be assigned id "1" — exactly the ambiguity
// docs/spec/atomic-bus.md's "ids stay unique across a daemon restart"
// criterion exists to close.
//
// ids.ShortID draws 2 random bytes (65536 values) per call — not adequate
// on its own for a room log that persists indefinitely and can accumulate
// thousands of messages over its lifetime; the birthday bound puts a 50%
// collision chance at only a few hundred ids. Two draws concatenated widen
// the space to 32 bits (~4.3 billion), while still reusing ids.ShortID
// rather than a second random generator. usedIDs is this Room's own
// collision guard on top of that: a duplicate draw (astronomically
// unlikely, but cheap to rule out) is retried rather than silently
// producing two envelopes that share an id within one daemon's lifetime.
func (r *Room) nextEnvelopeID() (string, error) {
	for attempt := 0; attempt < maxIDGenAttempts; attempt++ {
		a, err := randomIDHalf(messageIDPrefix)
		if err != nil {
			return "", fmt.Errorf("bus: generate envelope id: %w", err)
		}
		b, err := randomIDHalf(messageIDPrefix)
		if err != nil {
			return "", fmt.Errorf("bus: generate envelope id: %w", err)
		}
		id := messageIDPrefix + "-" + a + b
		if _, seen := r.usedIDs[id]; !seen {
			r.usedIDs[id] = struct{}{}
			return id, nil
		}
	}
	return "", &Error{Code: ExitHard, Msg: "bus: could not generate a unique envelope id after retrying"}
}

// randomIDHalf draws 4 lowercase hex characters via ids.ShortID, discarding
// the "<prefix>-" ShortID always adds — nextEnvelopeID composes two draws
// into one wider id rather than trusting a single draw's 65536-value space.
func randomIDHalf(prefix string) (string, error) {
	id, err := ids.ShortID(prefix)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(id, prefix+"-"), nil
}

// fanOut delivers env to every live subscriber without blocking the
// publisher, except a subscriber whose skipSelf is set and whose session
// matches publisherSession — that subscriber is skipped entirely, silently
// and without touching its drop count, because it was never meant to
// receive this envelope in the first place (docs/spec/atomic-bus.md: "a
// subscriber does not receive its own published messages"). An empty
// publisherSession (operator publishes, halt/resume control envelopes)
// never matches any subscriber's session, since a real session id is never
// empty — see Subscribe's doc.
//
// For everyone else, each subscriber's channel is buffered
// (subscriberBuffer); a full channel means that subscriber is falling
// behind or its reader has stopped, so the send is dropped rather than
// blocking — Publish must never stall because one reader stopped reading. A
// drop is never silent to the subscriber that missed it: each one tracks
// its own drop count, and the next envelope that does fit in its buffer is
// preceded by a synthetic control envelope (From systemName) naming how
// many were dropped and the room log path where they remain durably
// recorded — so a subscriber can always tell "nothing was sent" from "you
// missed N".
func (r *Room) fanOut(env Envelope, home string, publisherSession string) {
	for _, sub := range r.subs {
		if sub.skipSelf && publisherSession != "" && sub.session == publisherSession {
			continue
		}
		if sub.dropped > 0 {
			marker := r.dropMarkerEnvelope(env.Room, home, sub.dropped)
			if !trySend(sub.ch, marker) {
				// No room even for the marker; env won't fit either. Leave
				// dropped as-is (plus this env) and try again next publish.
				sub.dropped++
				continue
			}
			sub.dropped = 0
		}
		if !trySend(sub.ch, env) {
			sub.dropped++
		}
	}
}

// dropMarkerEnvelope builds the synthetic control envelope fanOut delivers
// ahead of the next real one once a subscriber has missed messages. It is
// never appended to the room log — it exists only on the one
// subscriber's live stream that actually missed something, and other
// subscribers of the same room may never see one at all. A ShortID failure
// here (rand exhausted — not realistically reachable) falls back to an
// empty id rather than dropping the marker itself: this envelope is never
// looked up by id (never logged, never replayed — there is no replay of
// any kind), so an empty id costs nothing.
func (r *Room) dropMarkerEnvelope(room, home string, n int) Envelope {
	id, err := r.nextEnvelopeID()
	if err != nil {
		id = ""
	}
	return Envelope{
		ID:       id,
		Room:     room,
		From:     systemName,
		FromKind: kindSystem,
		Ts:       time.Now(),
		Text:     fmt.Sprintf("bus: %d message(s) dropped for this subscriber while its buffer was full; nothing is lost — see the room log", n),
		Log:      RoomLogPath(home, room),
	}
}

func trySend(ch chan<- Envelope, env Envelope) bool {
	select {
	case ch <- env:
		return true
	default:
		return false
	}
}
