package bus

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/ids"
)

// subscriberBuffer bounds each subscriber's delivery channel; a full channel
// drops rather than blocks (Room.fanOut).
const subscriberBuffer = 32

// Hub owns every room a running daemon knows about, behind one mutex. One
// mutex rather than one per room: "never two backends in one room" is a
// compare-and-swap against the roster, atomic only if no other room state can
// change in between. The cost is that Publish and setHalted hold h.mu across
// their room-log writes, so one room's slow write blocks every other room.
type Hub struct {
	home string
	now  func() time.Time

	mu    sync.Mutex
	rooms map[string]*Room
}

// NewHub creates a Hub whose room logs are written under home (RoomLogPath).
// Its clock defaults to time.Now; see SetClock.
func NewHub(home string) *Hub {
	return &Hub{home: home, now: time.Now, rooms: map[string]*Room{}}
}

// SetClock overrides Hub's time source so staleness tests can advance "now"
// without sleeping. Call once immediately after NewHub — not safe once the Hub
// is serving concurrent requests.
func (h *Hub) SetClock(now func() time.Time) {
	h.now = now
}

// Room is one named room's authoritative state: who's in it, whether it's
// halted, and who is subscribed to its live traffic. It has no lock of its
// own — every field is guarded by the owning Hub's mutex, and every method
// here assumes that lock is already held.
type Room struct {
	members   map[string]Member // by assigned name
	bySession map[string]string // session id -> assigned name

	halted bool
	// Retained rather than only broadcast at halt time, so rooms/who/status can
	// report why a room is halted later, including after Rehydrate.
	haltReason string

	// usedIDs is nextEnvelopeID's collision guard, scoped to this daemon's
	// lifetime.
	usedIDs map[string]struct{}

	subs   map[int]*subscriber
	subSeq int
}

// subscriber pairs a delivery channel with its own drop count and the session
// it was opened for; the subscribe call itself carries no identity. dropped is
// only ever touched from fanOut, always under the owning Hub's mutex.
type subscriber struct {
	ch       chan<- Envelope
	dropped  int
	session  string
	skipSelf bool
}

// getOrCreateRoom returns the named room, creating it on first touch. Caller
// must hold h.mu.
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

// systemName and operatorName are the sentinel From values Join refuses to
// assign (reservedNames), which is what makes them proof rather than
// convention: From == systemName is a daemon control envelope, From ==
// operatorName is a human operator. The operator proof is load-bearing — the
// skill tells agents to treat operator messages as authoritative user input,
// so a forgeable operator identity is privilege escalation between agents.
// kindSystem sits outside validKind's enum, so FromKind == kindSystem is
// unspoofable the same way.
const (
	systemName   = "system"
	kindSystem   = "system"
	operatorName = "human"
)

// reservedNames are the sentinel From values no member may claim at Join. A
// new sentinel anywhere in the package belongs here.
var reservedNames = map[string]bool{
	systemName:   true,
	operatorName: true,
}

// validKind closes Member.Kind to exactly two values. Kind is wire input, so
// an unknown value must be a clean protocol error rather than something stored
// or panicked on; Publish's halt check relies on the enum being closed.
func validKind(kind string) bool {
	return kind == KindAgent || kind == KindHuman
}

// Join claims name in room, atomically: taken -> retry once as "<name>-2" ->
// still taken -> ExitNameTaken. The check and the claim run as one critical
// section, which is what stops two "backend"s ever existing in one room.
//
// A failed Join is a no-op on the roster: session's prior entry is released
// only once the new name is known claimable, so a rejoin can never leave a
// session holding two entries, and a failed one can never leave it holding
// zero.
//
// repo and realm are the client's own reported position, stored verbatim —
// unlike From at publish time there is no roster entry yet to check them
// against. Empty realm is valid and never rewritten to a placeholder.
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

// nameAvailableTo reports whether candidate is free, or already held by session
// itself, so re-asserting your own name is never blocked by your own entry.
func (r *Room) nameAvailableTo(candidate, session string) bool {
	m, taken := r.members[candidate]
	return !taken || m.Session == session
}

// Rehydrate rebuilds the whole roster from ~/.atomic/bus.json at Serve startup,
// so a member idle across a restart stays present and addressable — the
// per-client re-registration it replaced could only restore a session that ran
// a command.
//
// It bypasses Join's collision retry: a restored member owns its name by right,
// it is not a new claim racing a freshly empty roster. Kind and Mode fall back
// to Join's own defaults for entries persisted before those fields existed.
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
			// Restored as persisted, never restamped to "now": restamping
			// resurrects a long-dead session as freshly live and puts it
			// permanently out of Prune's reach. Zero predates the field, and
			// Joined is the best stand-in because it is never zero.
			lastSeen := m.LastSeen
			if lastSeen.IsZero() {
				lastSeen = m.Joined
			}
			r := h.getOrCreateRoom(room)
			r.members[m.Name] = Member{Name: m.Name, Kind: kind, Mode: mode, Session: session, Joined: m.Joined, LastSeen: lastSeen, Repo: m.Repo, Realm: m.Realm}
			r.bySession[session] = m.Name
		}
	}

	// Halt is room-level, not tied to any membership — restore it separately so
	// a halted room comes back halted even when empty.
	for room, rs := range st.Rooms {
		if rs == nil || !rs.Halted {
			continue
		}
		r := h.getOrCreateRoom(room)
		r.halted = true
		r.haltReason = rs.HaltText
	}
}

// UnknownAddressees reports which entries of to are not currently members of
// room. It never blocks or alters delivery: a named member may legitimately be
// about to join, so Publish still sends unconditionally.
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

// Leave removes session's membership, dropping the room when that was its last
// member and nothing is subscribed.
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

// dropIfEmpty removes a room with no members and no live subscribers. The
// subscriber check is what keeps this from yanking a room out from under a live
// tail or recv: those hold no membership, and dropping the Room here would
// orphan them, since the next Publish would build a fresh Room with an empty
// subs map. Caller must hold h.mu.
func (h *Hub) dropIfEmpty(room string, r *Room) bool {
	if len(r.members) > 0 || len(r.subs) > 0 {
		return false
	}
	delete(h.rooms, room)
	return true
}

// Who returns room's roster, sorted by name. Stale is computed fresh against
// the current clock at query time, never persisted.
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

// staleThreshold is how long a member may go without fresh LastSeen activity
// before who/prune consider it stale. A judgment call — long enough that a
// normal quiet spell is not flagged, short enough that `who` stays useful — so
// it is named here to be revisited without touching the staleness logic. A
// member holding an open subscription is never stale regardless (isStale), so
// this only bites a join with no following recv.
const staleThreshold = 10 * time.Minute

// isStale reports whether m should be treated as gone. A live subscription
// overrides LastSeen entirely: it is ongoing proof of life, however long the
// member has been quiet. Caller must hold h.mu.
func (r *Room) isStale(m Member, now time.Time) bool {
	if now.Sub(m.LastSeen) <= staleThreshold {
		return false
	}
	return !r.hasLiveSubscription(m.Session)
}

// hasLiveSubscription reports whether an open subscription in this room belongs
// to session. An empty session (operator publishes, tail) never counts — Join
// always assigns one.
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

// Prune removes every stale member of room and reports their names, sorted.
// The one place a member is removed without that session asking, so it is
// operator-invoked and never automatic: a quiet session is not a dead one, and
// evicting a live member breaks addressing with no diagnostic.
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

// Rooms summarizes every room the Hub knows about (created by a join or a
// subscribe), sorted by name. A room emptied by its last member leaving is
// still reported, with Members == 0.
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
// from_realm from the sender's roster membership and never from the request,
// appends unconditionally to the durable room log, and fans out.
//
// Halt is enforced here, not merely advertised: an advisory flag is exactly
// what a looping agent would ignore. Written as "!= KindHuman" rather than the
// equivalent "== KindAgent" so a future third kind fails closed.
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

	// A successful send counts as activity. member is a map value, not a
	// pointer, so the touched copy must be written back.
	now := h.now()
	member.LastSeen = now
	r.members[name] = member

	resolvedTo, err := r.resolveAddressees(to)
	if err != nil {
		return Envelope{}, err
	}
	return r.publishValidated(h.home, room, name, member.Kind, member.Repo, member.Realm, resolvedTo, replyTo, text, session, now)
}

// PublishAsOperator publishes into an already-existing room without holding a
// membership — the path `say` uses to speak into a room without occupying a
// roster slot or appearing in `who`.
//
// The identity is fixed, deliberately not a parameter: an earlier signature
// read name and kind from the wire, which let any local process publish under
// an existing agent's name and, since this path skips the halt check, speak
// into a halted room. Skipping halt is correct only because the identity is
// pinned — halt binds agents, and a human is who lifts it.
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
	// "" publisherSession: an operator publish is tied to no subscription, so it
	// can never wrongly trip a subscriber's skipSelf check in fanOut. "" repo and
	// realm: say never joins, so there is no Member to read a position from.
	return r.publishValidated(h.home, room, operatorName, KindHuman, "", "", resolvedTo, replyTo, text, "", h.now())
}

// resolveAddressees resolves every entry of to against r's membership. An
// ambiguous entry aborts the whole send — never a partial publish under a
// half-resolved to list. Caller must hold h.mu.
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

// resolveOneAddressee resolves one --to entry: exact name first, then a unique
// substring, so "--to fe-main" reaches "taxgentic-gui-fe-main" without typing
// the whole stacked name. Exact wins outright so a "-2" collision sibling can
// still be addressed precisely. Several matches is an error naming every
// candidate, because a silent pick among them is the failure this whole scheme
// exists to avoid. Zero matches passes through unresolved, leaving
// UnknownAddressees's softer warning — which still delivers — to cover a typo
// or a peer about to join. Caller must hold h.mu.
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

// publishValidated is the shared tail of Publish and PublishAsOperator: with
// the sender identity resolved and any halt check cleared, it enforces the
// wire-size limits, assigns an id, appends to the durable room log, and fans
// out. publisherSession is "" for operator sends (fanOut's self-echo check).
// now is the single timestamp stamped onto both the envelope and the sender's
// LastSeen, so the two agree exactly. Caller must hold h.mu.
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

// Halt sets room's halt flag and publishes a control envelope announcing it.
// The caller need not be a joined member — an operator can stop a room they
// are not in — so the envelope's From is the systemName sentinel.
func (h *Hub) Halt(room, text string) error {
	return h.setHalted(room, true, text)
}

// Resume clears room's halt flag and publishes the clearing envelope.
func (h *Hub) Resume(room, text string) error {
	return h.setHalted(room, false, text)
}

// defaultResumeText fills an empty Resume body. Halt is left as given: a halt
// with no reason still carries the one fact that matters, where an empty resume
// notification carries nothing to act on.
const defaultResumeText = "room resumed"

// setHalted flips r.halted only once the announcing envelope is durably
// appended. Flipping first would leave the room genuinely halted with no
// control envelope logged or broadcast, while the returned error implied the
// halt might not have taken.
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
	// "" publisherSession: a control envelope is never a member's own send.
	r.fanOut(env, h.home, "")
	return nil
}

// IsHalted reports room's halt flag and the reason given at halt time (empty
// when not halted, or when halted with no --text).
func (h *Hub) IsHalted(room string) (halted bool, reason string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, ok := h.getRoom(room)
	if !ok {
		return false, "", noRoomError(room)
	}
	return r.halted, r.haltReason, nil
}

// Close publishes a final "room closed" envelope, evicts every member, ends
// every live subscriber's stream, and drops the room from the Hub. Operator-
// level, needing no prior membership. The room log on disk is never touched:
// it is the durable record, and a roster operation must not delete it.
//
// Closing each subscriber channel — rather than only unregistering it, as
// dropIfEmpty does — is deliberate: a listener should learn why it stopped.
// daemon.go's subscribe loop checks the receive ok value so this terminates
// the connection instead of spinning on a closed channel.
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

// SessionIsMember reports whether session holds a membership in room — the
// check daemon.go's OpRecv dispatch makes before trusting a client-claimed
// session. The socket has no authentication beyond Unix file permissions, so
// this cannot close every spoofing path; what it does close is a subscription
// opened before its session joined, which could otherwise sit in r.subs and
// attach itself to whoever joins under that name later, silently keeping them
// non-stale. Empty session or unknown room both report false.
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

// Subscribe registers ch for every future Publish on room (control envelopes
// included), creating the room if needed — tail may watch a room before anyone
// has joined it. session feeds fanOut's self-echo check and
// hasLiveSubscription; pass "" when the caller has no session of its own.
// Subscribe trusts session verbatim — OpRecv's dispatch, via SessionIsMember,
// is what downgrades an unowned claim to "" before it reaches here. skipSelf,
// meaningful only when session is set, opts the subscription out of its own
// session's publishes. The returned func removes the subscription and must be
// called exactly once.
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

// messageIDPrefix prefixes every opaque envelope id nextEnvelopeID assigns,
// e.g. "m-3f2ab71c".
const messageIDPrefix = "m"

// maxIDGenAttempts bounds nextEnvelopeID's collision-retry loop.
const maxIDGenAttempts = 5

// nextEnvelopeID assigns a short opaque id. Ids must stay unique across a
// daemon restart, which the sequential per-process counter this replaced could
// not do: the room log outlives the daemon, so two lifetimes both minted "1".
// ids.ShortID draws only 2 random bytes, whose birthday bound collides within a
// few hundred ids, so two draws are concatenated to 32 bits; usedIDs rules out
// the remainder within one daemon's lifetime.
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

// randomIDHalf draws 4 lowercase hex characters via ids.ShortID, discarding the
// "<prefix>-" ShortID always prepends.
func randomIDHalf(prefix string) (string, error) {
	id, err := ids.ShortID(prefix)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(id, prefix+"-"), nil
}

// fanOut delivers env to every live subscriber without blocking the publisher.
// A subscriber with skipSelf whose session matches publisherSession is skipped
// silently, without touching its drop count — it was never meant to receive
// this envelope. An empty publisherSession matches nobody, since a real session
// id is never empty.
//
// A full channel means that subscriber's reader has stalled, so the send is
// dropped rather than blocking — Publish must never stall on one reader. Drops
// are never silent: each subscriber tracks its own count, and the next envelope
// that fits is preceded by a marker naming how many were missed and the room
// log where they remain.
func (r *Room) fanOut(env Envelope, home string, publisherSession string) {
	for _, sub := range r.subs {
		if sub.skipSelf && publisherSession != "" && sub.session == publisherSession {
			continue
		}
		if sub.dropped > 0 {
			marker := r.dropMarkerEnvelope(env.Room, home, sub.dropped)
			if !trySend(sub.ch, marker) {
				// No room even for the marker; env won't fit either.
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

// dropMarkerEnvelope builds the synthetic envelope fanOut delivers ahead of the
// next real one once a subscriber has missed messages. Never appended to the
// room log — it exists only on the one stream that missed something. An id
// failure falls back to an empty id rather than dropping the marker: this
// envelope is never logged, replayed, or looked up by id.
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
