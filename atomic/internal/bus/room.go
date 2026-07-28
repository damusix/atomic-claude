package bus

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ringCapacity bounds each room's in-memory replay buffer. It is the
// only durable-in-process history a `--since` catch-up can serve without
// touching disk; the room log (roomlog.go) is the actual durable record.
const ringCapacity = 256

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

	mu    sync.Mutex
	rooms map[string]*Room
}

// NewHub creates a Hub whose room logs are written under home (see
// RoomLogPath in paths.go).
func NewHub(home string) *Hub {
	return &Hub{home: home, rooms: map[string]*Room{}}
}

// Room is one named room's authoritative state: who's in it, its bounded
// replay ring, whether it's halted, and who is currently subscribed to its
// live traffic.
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

	ring    []Envelope // fixed-length circular buffer, len == ringCapacity
	ringPos int        // next write index
	ringLen int        // number of valid entries currently in ring
	nextID  uint64

	subs   map[int]*subscriber
	subSeq int
}

// subscriber pairs a live subscriber's delivery channel with its own drop
// count. dropped is only ever touched from fanOut, which always runs under
// the owning Hub's mutex (see Room's doc comment) — no separate lock needed.
type subscriber struct {
	ch      chan<- Envelope
	dropped int
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
			ring:      make([]Envelope, ringCapacity),
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
const (
	systemName = "system"
	kindSystem = "system"
)

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
func (h *Hub) Join(room, name, mode, kind, session string) (string, error) {
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
	if name == systemName {
		return "", &Error{
			Code: ExitUsage,
			Msg:  fmt.Sprintf("bus: name %q is reserved for daemon control envelopes", systemName),
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

	r.members[assigned] = Member{Name: assigned, Kind: kind, Mode: mode, Session: session, Joined: time.Now()}
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

// Leave removes session's membership from room.
func (h *Hub) Leave(room, session string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return noRoomError(room)
	}
	name, ok := r.bySession[session]
	if !ok {
		return &Error{Code: ExitNotJoined, Msg: fmt.Sprintf("bus: session has not joined room %q", room)}
	}
	delete(r.members, name)
	delete(r.bySession, session)
	return nil
}

// Who returns room's current roster, sorted by name for stable output.
func (h *Hub) Who(room string) ([]Member, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return nil, noRoomError(room)
	}
	out := make([]Member, 0, len(r.members))
	for _, m := range r.members {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Rooms returns the names of every room the Hub currently knows about
// (created by a join or a subscribe), sorted.
func (h *Hub) Rooms() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]string, 0, len(h.rooms))
	for name := range h.rooms {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Publish assigns an id and timestamp, stamps from/from_kind from the
// sender's roster membership, appends unconditionally to the durable room
// log, pushes onto the bounded in-memory ring, and fans out to live
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

	env := Envelope{
		ID:       r.nextEnvelopeID(),
		Room:     room,
		From:     name,
		FromKind: member.Kind,
		To:       to,
		ReplyTo:  replyTo,
		Ts:       time.Now(),
		Text:     text,
	}

	if err := Append(h.home, room, env); err != nil {
		return Envelope{}, fmt.Errorf("bus: append room log: %w", err)
	}

	r.pushRing(env)
	r.fanOut(env, h.home)
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

// Resume clears room's halt flag and publishes the clearing envelope.
func (h *Hub) Resume(room, text string) error {
	return h.setHalted(room, false, text)
}

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

	env := Envelope{
		ID:       r.nextEnvelopeID(),
		Room:     room,
		From:     systemName,
		FromKind: KindHuman,
		Ts:       time.Now(),
		Text:     text,
	}
	if err := Append(h.home, room, env); err != nil {
		return fmt.Errorf("bus: append room log: %w", err)
	}

	r.halted = halted
	r.pushRing(env)
	r.fanOut(env, h.home)
	return nil
}

// IsHalted reports whether room currently has its halt flag set.
func (h *Hub) IsHalted(room string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return false, noRoomError(room)
	}
	return r.halted, nil
}

// Since returns every envelope in room's ring after the one whose id is
// since, or every envelope currently in the ring if since is empty or no
// longer present (evicted by the ringCapacity cap) — an evicted id is not
// an error, per docs/spec/atomic-bus.md's Since row; the caller notices a
// gap by comparing the id it asked for against what came back.
func (h *Hub) Since(room, since string) ([]Envelope, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return nil, noRoomError(room)
	}
	return r.since(since), nil
}

// Subscribe registers ch to receive every future Publish (including
// Halt/Resume's control envelopes) on room, creating room if it doesn't
// exist yet — tail may watch a room before anyone has joined it (see
// docs/design/atomic-bus.md's decision #5: tail never joins and holds no
// name). The returned func removes the subscription; callers must invoke
// it exactly once, typically via defer, when the subscribing connection
// ends.
func (h *Hub) Subscribe(room string, ch chan<- Envelope) func() {
	h.mu.Lock()
	r := h.getOrCreateRoom(room)
	id := r.subSeq
	r.subSeq++
	r.subs[id] = &subscriber{ch: ch}
	h.mu.Unlock()

	return func() {
		h.mu.Lock()
		delete(r.subs, id)
		h.mu.Unlock()
	}
}

// --- Room internals. All of the following assume h.mu is already held. ---

func (r *Room) nextEnvelopeID() string {
	r.nextID++
	return strconv.FormatUint(r.nextID, 36)
}

func (r *Room) pushRing(env Envelope) {
	r.ring[r.ringPos] = env
	r.ringPos = (r.ringPos + 1) % ringCapacity
	if r.ringLen < ringCapacity {
		r.ringLen++
	}
}

// ringSnapshot returns the ring's contents oldest-to-newest.
func (r *Room) ringSnapshot() []Envelope {
	out := make([]Envelope, 0, r.ringLen)
	start := (r.ringPos - r.ringLen + ringCapacity) % ringCapacity
	for i := 0; i < r.ringLen; i++ {
		out = append(out, r.ring[(start+i)%ringCapacity])
	}
	return out
}

func (r *Room) since(id string) []Envelope {
	all := r.ringSnapshot()
	if id == "" {
		return all
	}
	for i, env := range all {
		if env.ID == id {
			return append([]Envelope{}, all[i+1:]...)
		}
	}
	// id not found: either evicted or never existed. Return what remains
	// rather than erroring — see the Since doc comment above.
	return all
}

// fanOut delivers env to every live subscriber without blocking the
// publisher. Each subscriber's channel is buffered (subscriberBuffer); a
// full channel means that subscriber is falling behind or its reader has
// stopped, so the send is dropped rather than blocking — Publish must
// never stall because one reader stopped reading. A drop is never silent
// to the subscriber that missed it: each one tracks its own drop count,
// and the next envelope that does fit in its buffer is preceded by a
// synthetic control envelope (From systemName) naming how many were dropped
// and the room log path where they remain durably recorded — so a
// subscriber can always tell "nothing was sent" from "you missed N".
func (r *Room) fanOut(env Envelope, home string) {
	for _, sub := range r.subs {
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
// never appended to the room log or ring — it exists only on the one
// subscriber's live stream that actually missed something, and other
// subscribers of the same room may never see one at all.
func (r *Room) dropMarkerEnvelope(room, home string, n int) Envelope {
	return Envelope{
		ID:       r.nextEnvelopeID(),
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
