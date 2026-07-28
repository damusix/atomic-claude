package bus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// subscribeTimeout bounds every subscriber-channel assertion in this file.
// A missed publish must fail the test with a clear message, never hang
// the suite — see the atomic-bus brief's concurrency success criteria.
const subscribeTimeout = 2 * time.Second

func recvEnvelope(t *testing.T, ch <-chan Envelope) Envelope {
	t.Helper()
	select {
	case env := <-ch:
		return env
	case <-time.After(subscribeTimeout):
		t.Fatalf("timed out after %s waiting for a published envelope", subscribeTimeout)
		return Envelope{}
	}
}

func mustError(t *testing.T, err error, want ExitCode) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error with code %d, got nil", want)
	}
	busErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *bus.Error, got %T: %v", err, err)
	}
	if busErr.Code != want {
		t.Fatalf("Code = %d, want %d (%v)", busErr.Code, want, busErr)
	}
	return busErr
}

// --- Join: the atomic name claim ---

func TestHub_Join_FirstClaimGetsExactName(t *testing.T) {
	h := NewHub(t.TempDir())

	name, err := h.Join("potato", "backend", "normal", "agent", "sess-1")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if name != "backend" {
		t.Fatalf("name = %q, want %q", name, "backend")
	}
}

func TestHub_Join_SecondClaimOfSameNameGetsNumericSuffix(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("first Join: %v", err)
	}

	name, err := h.Join("potato", "backend", "normal", "agent", "sess-2")
	if err != nil {
		t.Fatalf("second Join: %v", err)
	}
	if name != "backend-2" {
		t.Fatalf("name = %q, want %q", name, "backend-2")
	}
}

func TestHub_Join_ThirdClaimOfSameNameFailsWithNameTaken(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-2"); err != nil {
		t.Fatalf("second Join: %v", err)
	}

	_, err := h.Join("potato", "backend", "normal", "agent", "sess-3")
	mustError(t, err, ExitNameTaken)
}

func TestHub_Join_SameNameDifferentRoomsDoNotCollide(t *testing.T) {
	h := NewHub(t.TempDir())

	name1, err := h.Join("potato", "backend", "normal", "agent", "sess-1")
	if err != nil {
		t.Fatalf("Join room potato: %v", err)
	}
	name2, err := h.Join("carrot", "backend", "normal", "agent", "sess-2")
	if err != nil {
		t.Fatalf("Join room carrot: %v", err)
	}
	if name1 != "backend" || name2 != "backend" {
		t.Fatalf("names = %q, %q; want both %q (rooms are independent namespaces)", name1, name2, "backend")
	}
}

// TestHub_Join_RejoiningReleasesPriorName proves a session that joins the
// same room twice does not leak a stale roster entry under its old name —
// otherwise a retried join would silently accumulate ghost members that
// Who would report as still present.
func TestHub_Join_RejoiningReleasesPriorName(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("second Join (same session): %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected exactly one member after a re-join, got %d: %+v", len(members), members)
	}
}

// TestHub_Join_FailedRejoinLeavesRosterAndPublishIntact reproduces the
// checkpoint 2 review's headline bug: Join used to delete a session's
// prior roster entry before confirming the new name was claimable, so a
// Join that failed with ExitNameTaken left bySession pointing at a name no
// longer in members — Who() undercounted, and the next Publish from that
// session carried a stale From with an empty FromKind. A failed Join must
// be a no-op on the roster.
func TestHub_Join_FailedRejoinLeavesRosterAndPublishIntact(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join backend: %v", err)
	}
	if _, err := h.Join("potato", "worker", "normal", "agent", "sess-2"); err != nil {
		t.Fatalf("Join worker: %v", err)
	}
	if _, err := h.Join("potato", "worker", "normal", "agent", "sess-3"); err != nil {
		t.Fatalf("Join worker-2: %v", err)
	}

	// sess-1 attempts to rejoin as "worker", which is taken in both its
	// bare and "-2" forms by sess-2 and sess-3.
	_, err := h.Join("potato", "worker", "normal", "agent", "sess-1")
	mustError(t, err, ExitNameTaken)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("roster has %d members after the failed rejoin, want 3 (unchanged): %+v", len(members), members)
	}
	found := false
	for _, m := range members {
		if m.Name == "backend" {
			found = true
			if m.Session != "sess-1" {
				t.Errorf("backend's Session = %q, want %q", m.Session, "sess-1")
			}
		}
	}
	if !found {
		t.Fatal("expected sess-1 to still appear in Who() as \"backend\" after the failed rejoin")
	}

	env, err := h.Publish("potato", "sess-1", nil, "", "still here")
	if err != nil {
		t.Fatalf("Publish after failed rejoin: %v", err)
	}
	if env.From != "backend" {
		t.Errorf("From = %q, want %q", env.From, "backend")
	}
	if env.FromKind != "agent" {
		t.Errorf("FromKind = %q, want non-empty %q (a corrupted roster leaves this zero-valued)", env.FromKind, "agent")
	}
}

// TestHub_Join_Concurrent_ExactlyOneKeepsExactNameOneGetsSuffixRestFail is
// the load-bearing test of this checkpoint: the name claim must be atomic,
// not merely unlikely to collide. N goroutines race to join the same room
// under the same name; the atomicity guarantee is that the outcome
// distribution is exactly {1 exact-name winner, 1 "-2" winner, N-2
// ExitNameTaken failures} — never two exact-name winners, never two "-2"
// winners, never an outcome that depends on scheduling. Run with
// -race: the whole point of Hub.mu is that this can't race.
func TestHub_Join_Concurrent_ExactlyOneKeepsExactNameOneGetsSuffixRestFail(t *testing.T) {
	const n = 20
	h := NewHub(t.TempDir())

	var wg sync.WaitGroup
	results := make([]struct {
		name string
		err  error
	}, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := "sess-" + string(rune('a'+i))
			name, err := h.Join("potato", "backend", "normal", "agent", session)
			results[i].name = name
			results[i].err = err
		}(i)
	}
	wg.Wait()

	var exactWins, suffixWins, nameTakenFails, otherFails int
	for _, r := range results {
		switch {
		case r.err == nil && r.name == "backend":
			exactWins++
		case r.err == nil && r.name == "backend-2":
			suffixWins++
		case r.err != nil:
			if busErr, ok := r.err.(*Error); ok && busErr.Code == ExitNameTaken {
				nameTakenFails++
			} else {
				otherFails++
			}
		default:
			otherFails++
		}
	}

	if exactWins != 1 {
		t.Errorf("exact-name winners = %d, want exactly 1", exactWins)
	}
	if suffixWins != 1 {
		t.Errorf("suffix winners = %d, want exactly 1", suffixWins)
	}
	if nameTakenFails != n-2 {
		t.Errorf("ExitNameTaken failures = %d, want %d", nameTakenFails, n-2)
	}
	if otherFails != 0 {
		t.Errorf("unexpected non-ExitNameTaken outcomes = %d", otherFails)
	}

	// The roster itself must agree with the outcome: exactly two members,
	// never more (a lost update would manifest as a missing or duplicated
	// roster entry even if the returned names looked right above).
	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("roster has %d members after the race, want exactly 2: %+v", len(members), members)
	}
}

// TestHub_Join_ReservedSystemNameRejected reproduces the round 3 review's
// finding 1: before this fix, a client could Join as "system" and every
// subsequent Publish from it would carry From:"system" — bit-for-bit
// identical to a daemon control envelope (setHalted's announcement,
// fanOut's drop marker), defeating the guarantee those exist for. A
// rejected Join must also be a no-op: the room's existing roster is
// untouched.
func TestHub_Join_ReservedSystemNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join backend: %v", err)
	}

	_, err := h.Join("potato", "system", "normal", "agent", "sess-2")
	mustError(t, err, ExitUsage)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected only the original member after a rejected Join, got %d: %+v", len(members), members)
	}
}

// TestHub_Join_InvalidKindRejected is the other half of finding 1: kind was
// previously unvalidated, so a client could Join with any string and have
// it stamped verbatim onto every envelope's from_kind — including "system",
// which is exactly what let a spoofed member match fanOut's drop marker
// (From:"system", FromKind:"system"). kind must be one of the two
// documented values (protocol.go's KindAgent/KindHuman) or Join fails
// cleanly, never silently stores the bogus value.
func TestHub_Join_InvalidKindRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Join("potato", "backend", "normal", "hologram", "sess-1")
	mustError(t, err, ExitUsage)

	if rooms := h.Rooms(); len(rooms) != 0 {
		t.Fatalf("expected no room created by a rejected Join, got %v", rooms)
	}
}

// TestHub_Join_OverLongRoomNameRejected is finding 2's Join-side half: an
// unbounded room name written into every envelope's Room field could
// overflow roomlog.go's scanner budget and break ReadSince for that room
// forever, the same failure class MaxTextBytes closed for Text.
func TestHub_Join_OverLongRoomNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	overlong := strings.Repeat("r", MaxIdentifierBytes+1)

	_, err := h.Join(overlong, "backend", "normal", "agent", "sess-1")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxIdentifierBytes)) {
		t.Errorf("error message %q does not name the %d-byte limit", busErr.Msg, MaxIdentifierBytes)
	}
	if rooms := h.Rooms(); len(rooms) != 0 {
		t.Fatalf("expected no room created by a rejected Join, got %v", rooms)
	}
}

// TestHub_Join_OverLongNameRejected mirrors the room-name case above for a
// member's assigned name, which lands in every envelope's From field.
func TestHub_Join_OverLongNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	overlong := strings.Repeat("n", MaxIdentifierBytes+1)

	_, err := h.Join("potato", overlong, "normal", "agent", "sess-1")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxIdentifierBytes)) {
		t.Errorf("error message %q does not name the %d-byte limit", busErr.Msg, MaxIdentifierBytes)
	}
}

// --- Rehydrate: restoring the roster from bus.json at daemon startup ---
//
// docs/spec/atomic-bus.md: "a restarted daemon rehydrates the whole roster
// ... not one session at a time" and "mode and kind survive a daemon
// restart".

func TestHub_Rehydrate_RestoresWholeRosterAcrossMultipleRoomsAndSessions(t *testing.T) {
	h := NewHub(t.TempDir())
	st := &State{Sessions: map[string]*sessionState{
		"sess-fe": {Rooms: map[string]roomMembership{
			"potato": {Name: "frontend", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
		"sess-be": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
		// judge never runs another command after the restart — rehydration
		// must not depend on that, unlike the per-session re-registration
		// this replaced.
		"sess-jd": {Rooms: map[string]roomMembership{
			"potato": {Name: "judge", Mode: "observe", Kind: KindAgent, Joined: time.Now()},
		}},
		"sess-op": {Rooms: map[string]roomMembership{
			"carrot": {Name: "operator", Mode: "participate", Kind: KindHuman, Joined: time.Now()},
		}},
	}}

	h.Rehydrate(st)

	potato, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who(potato): %v", err)
	}
	if len(potato) != 3 {
		t.Fatalf("potato members = %+v, want 3 (a member idle across the restart must still be present and addressable)", potato)
	}
	byName := map[string]Member{}
	for _, m := range potato {
		byName[m.Name] = m
	}
	if _, ok := byName["judge"]; !ok {
		t.Fatal("judge is missing from the rehydrated roster")
	}
	if got := byName["judge"].Mode; got != "observe" {
		t.Fatalf("judge.Mode = %q, want %q — an observe member must not silently come back as participate", got, "observe")
	}

	carrot, err := h.Who("carrot")
	if err != nil {
		t.Fatalf("Who(carrot): %v", err)
	}
	if len(carrot) != 1 || carrot[0].Name != "operator" || carrot[0].Kind != KindHuman {
		t.Fatalf("carrot members = %+v, want one human named operator", carrot)
	}
}

// TestHub_Rehydrate_DefaultsEmptyModeAndKindForPreExistingEntries covers a
// bus.json written before Mode/Kind existed on roomMembership: Rehydrate
// must default exactly as a fresh Join would (room.go's Hub.Join defaults),
// not leave the zero value on the roster.
func TestHub_Rehydrate_DefaultsEmptyModeAndKindForPreExistingEntries(t *testing.T) {
	h := NewHub(t.TempDir())
	st := &State{Sessions: map[string]*sessionState{
		"sess-1": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Joined: time.Now()},
		}},
	}}

	h.Rehydrate(st)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members = %+v, want 1", members)
	}
	if members[0].Mode != "participate" {
		t.Fatalf("Mode = %q, want %q (Join's own default)", members[0].Mode, "participate")
	}
	if members[0].Kind != KindAgent {
		t.Fatalf("Kind = %q, want %q (Join's own default)", members[0].Kind, KindAgent)
	}
}

// TestHub_Rehydrate_PreservesSuffixedNamesWithoutRenaming proves Rehydrate
// bypasses Join's numeric-suffix collision retry entirely: two sessions
// that originally collided on "backend" (the second became "backend-2")
// must come back under those exact names, never renamed again — a fresh
// Join replaying the same claims in map-iteration order would have no way
// to know "backend-2" was ever legitimately held by someone else.
func TestHub_Rehydrate_PreservesSuffixedNamesWithoutRenaming(t *testing.T) {
	h := NewHub(t.TempDir())
	st := &State{Sessions: map[string]*sessionState{
		"sess-1": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
		"sess-2": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend-2", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
	}}

	h.Rehydrate(st)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	got := map[string]bool{}
	for _, m := range members {
		got[m.Name] = true
	}
	if !got["backend"] || !got["backend-2"] {
		t.Fatalf("members = %+v, want backend and backend-2 both preserved verbatim", members)
	}
}

func TestHub_Rehydrate_EmptyStateLeavesHubWithNoRooms(t *testing.T) {
	h := NewHub(t.TempDir())
	h.Rehydrate(&State{Sessions: map[string]*sessionState{}})

	if rooms := h.Rooms(); len(rooms) != 0 {
		t.Fatalf("Rooms() = %+v, want none", rooms)
	}
}

// --- UnknownAddressees ---

func TestHub_UnknownAddressees_NamesEveryToEntryNotInTheRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	unknown := h.UnknownAddressees("potato", []string{"backend", "nobody-here"})
	if len(unknown) != 1 || unknown[0] != "nobody-here" {
		t.Fatalf("UnknownAddressees = %v, want [nobody-here]", unknown)
	}
}

func TestHub_UnknownAddressees_AllKnownReturnsEmpty(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if unknown := h.UnknownAddressees("potato", []string{"backend"}); len(unknown) != 0 {
		t.Fatalf("UnknownAddressees = %v, want none", unknown)
	}
}

func TestHub_UnknownAddressees_UnknownRoomReturnsEveryNameUnknown(t *testing.T) {
	h := NewHub(t.TempDir())

	unknown := h.UnknownAddressees("potato", []string{"backend"})
	if len(unknown) != 1 || unknown[0] != "backend" {
		t.Fatalf("UnknownAddressees = %v, want [backend] (a room that doesn't exist yet has no members)", unknown)
	}
}

// --- Leave / Who / Rooms ---

func TestHub_Leave_RemovesMemberFromRoster(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Leave("potato", "sess-1"); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no members after Leave, got %d", len(members))
	}
}

func TestHub_Leave_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	err := h.Leave("nonexistent", "sess-1")
	mustError(t, err, ExitNoRoom)
}

func TestHub_Leave_SessionNotMemberReturnsExitNotJoined(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	err := h.Leave("potato", "sess-stranger")
	mustError(t, err, ExitNotJoined)
}

func TestHub_Who_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Who("nonexistent")
	mustError(t, err, ExitNoRoom)
}

// TestHub_Rooms_ListsEveryKnownRoomSorted is also finding 4's regression:
// rooms must report a member count per room, not merely its name — potato
// and carrot are given different member counts specifically to catch a
// fix that reports a count but always the same (wrong) one.
func TestHub_Rooms_ListsEveryKnownRoomSorted(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join potato: %v", err)
	}
	if _, err := h.Join("carrot", "backend", "normal", "agent", "sess-2"); err != nil {
		t.Fatalf("Join carrot: %v", err)
	}
	if _, err := h.Join("carrot", "frontend", "normal", "agent", "sess-3"); err != nil {
		t.Fatalf("Join carrot (second member): %v", err)
	}

	got := h.Rooms()
	want := []RoomInfo{{Name: "carrot", Members: 2}, {Name: "potato", Members: 1}}
	if len(got) != len(want) {
		t.Fatalf("Rooms = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Rooms = %+v, want %+v", got, want)
		}
	}
}

// TestHub_Rooms_ReportsZeroMembersAfterEveryoneLeaves proves a room that
// has emptied is still listed (rooms persist after everyone leaves — see
// the daemon-level equivalent in daemon_test.go), now with an explicit
// Members == 0 rather than merely a bare name.
func TestHub_Rooms_ReportsZeroMembersAfterEveryoneLeaves(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := h.Leave("potato", "sess-1"); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	got := h.Rooms()
	if len(got) != 1 || got[0] != (RoomInfo{Name: "potato", Members: 0}) {
		t.Fatalf("Rooms = %+v, want [{potato 0}]", got)
	}
}

// --- Publish ---

func TestHub_Publish_AssignsIDStampsTsAndFromKind(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	before := time.Now()
	env, err := h.Publish("potato", "sess-1", []string{"backend"}, "", "status update")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if env.ID == "" {
		t.Error("expected a non-empty envelope id")
	}
	if env.From != "frontend" {
		t.Errorf("From = %q, want %q", env.From, "frontend")
	}
	if env.FromKind != "agent" {
		t.Errorf("FromKind = %q, want %q", env.FromKind, "agent")
	}
	if env.Ts.Before(before) {
		t.Errorf("Ts = %v, want at or after %v", env.Ts, before)
	}
	if env.Text != "status update" {
		t.Errorf("Text = %q, want %q", env.Text, "status update")
	}
}

// TestHub_Publish_ID_IsShortOpaqueString_NotSequential proves finding 2's
// documented wire shape ("id": "k2m9" — a short opaque string) rather than
// the sequential base36 counter this used to be. A sequential counter's
// first-ever id is always "1" regardless of format; this asserts the id is
// neither "1" nor purely numeric, so a regression back to a counter fails
// this test even if someone reformats the counter's base.
func TestHub_Publish_ID_IsShortOpaqueString_NotSequential(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	env, err := h.Publish("potato", "sess-1", nil, "", "hello")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if env.ID == "1" {
		t.Fatalf("ID = %q, want an opaque id, not the first sequential counter value", env.ID)
	}
	if !strings.HasPrefix(env.ID, messageIDPrefix+"-") {
		t.Fatalf("ID = %q, want it to start with %q", env.ID, messageIDPrefix+"-")
	}
	if _, convErr := strconv.ParseUint(env.ID, 10, 64); convErr == nil {
		t.Fatalf("ID = %q parses as a plain integer; want an opaque string", env.ID)
	}
}

// TestHub_Publish_IDsUniqueAcrossDaemonRestart is finding 2's core
// regression: a per-process sequential counter reset to zero on every
// daemon restart, so the first message published by a fresh Hub always got
// id "1" — indistinguishable from the first message published by the
// previous daemon's Hub, in the same durable room log. Two separate Hub
// instances (simulating two daemon lifetimes) publishing into the same
// room must never produce the same id.
func TestHub_Publish_IDsUniqueAcrossDaemonRestart(t *testing.T) {
	home := t.TempDir()

	h1 := NewHub(home)
	if _, err := h1.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join (daemon 1): %v", err)
	}
	env1, err := h1.Publish("potato", "sess-1", nil, "", "before restart")
	if err != nil {
		t.Fatalf("Publish (daemon 1): %v", err)
	}

	// A fresh Hub against the same home, exactly what a respawned daemon
	// constructs — its roster and id bookkeeping start over from nothing.
	h2 := NewHub(home)
	if _, err := h2.Join("potato", "frontend", "normal", "agent", "sess-2"); err != nil {
		t.Fatalf("Join (daemon 2): %v", err)
	}
	env2, err := h2.Publish("potato", "sess-2", nil, "", "after restart")
	if err != nil {
		t.Fatalf("Publish (daemon 2): %v", err)
	}

	if env1.ID == env2.ID {
		t.Fatalf("both daemon lifetimes assigned id %q to different messages in the same room log", env1.ID)
	}
}

// TestHub_Publish_ManyMessagesAllGetUniqueIDs is the collision-adequacy
// check the finding calls for: nextEnvelopeID's own usedIDs guard must
// hold under real volume, not merely for a couple of calls.
func TestHub_Publish_ManyMessagesAllGetUniqueIDs(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	const n = 2000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		env, err := h.Publish("potato", "sess-1", nil, "", "msg")
		if err != nil {
			t.Fatalf("Publish[%d]: %v", i, err)
		}
		if seen[env.ID] {
			t.Fatalf("id %q assigned twice within %d publishes", env.ID, n)
		}
		seen[env.ID] = true
	}
}

func TestHub_Publish_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Publish("nonexistent", "sess-1", nil, "", "hi")
	mustError(t, err, ExitNoRoom)
}

func TestHub_Publish_SessionNotMemberReturnsExitNotJoined(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	_, err := h.Publish("potato", "sess-stranger", nil, "", "hi")
	mustError(t, err, ExitNotJoined)
}

// TestHub_Publish_EveryEnvelopeLandsInRoomLog_EvenWithNoSubscribers locks
// in the durability contract: the room log is written unconditionally,
// not only when someone happens to be watching.
func TestHub_Publish_EveryEnvelopeLandsInRoomLog_EvenWithNoSubscribers(t *testing.T) {
	home := t.TempDir()
	h := NewHub(home)
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := h.Publish("potato", "sess-1", nil, "", "nobody is watching"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	path := RoomLogPath(home, "potato")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected room log at %s: %v", path, err)
	}
	var env Envelope
	if err := json.Unmarshal(raw[:len(raw)-1], &env); err != nil { // trim trailing newline
		t.Fatalf("room log line is not valid JSON: %v (%s)", err, raw)
	}
	if env.Text != "nobody is watching" {
		t.Fatalf("logged Text = %q, want %q", env.Text, "nobody is watching")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat room log: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("room log perm = %o, want 0600", perm)
	}
}

// TestHub_Publish_SlowSubscriberDoesNotBlockPublisher is the other
// concurrency-shaped correctness property this checkpoint calls out
// explicitly: a subscriber that never drains its channel must not be able
// to stall Publish. Bounded by a timeout so a regression to a blocking
// send hangs this test instead of the whole suite.
func TestHub_Publish_SlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A subscriber channel nobody ever reads from.
	deadCh := make(chan Envelope) // unbuffered on purpose: any blocking
	// send here would hang forever without the non-blocking fanOut.
	unsub := h.Subscribe("potato", deadCh)
	defer unsub()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// subscriberBuffer+several more, so this would overflow a
		// blocking-send implementation many times over.
		for i := 0; i < subscriberBuffer*3; i++ {
			if _, err := h.Publish("potato", "sess-1", nil, "", "msg"); err != nil {
				t.Errorf("Publish: %v", err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(subscribeTimeout):
		t.Fatal("Publish appears to have blocked on a slow/dead subscriber")
	}
}

// TestHub_Publish_OversizedTextRejected proves a message over
// MaxTextBytes is rejected at Publish, before it ever reaches the room
// log — the deterministic fix for a >1MB message otherwise breaking
// ReadSince for that room forever (roomlog.go's scanner buffer).
func TestHub_Publish_OversizedTextRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	oversized := strings.Repeat("a", MaxTextBytes+1)
	_, err := h.Publish("potato", "sess-1", nil, "", oversized)
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxTextBytes)) {
		t.Errorf("error message %q does not name the %d-byte limit", busErr.Msg, MaxTextBytes)
	}
}

// TestHub_Publish_OverLongReplyToRejected is finding 2's Publish-side cap on
// ReplyTo, the same failure class as the oversized-Text test above but for
// a field MaxTextBytes never covered.
func TestHub_Publish_OverLongReplyToRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	overlong := strings.Repeat("r", MaxIdentifierBytes+1)
	_, err := h.Publish("potato", "sess-1", nil, overlong, "hi")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxIdentifierBytes)) {
		t.Errorf("error message %q does not name the %d-byte limit", busErr.Msg, MaxIdentifierBytes)
	}
}

// TestHub_Publish_TooManyAddresseesRejected caps the addressee count
// independently of their combined length — see MaxAddressees's doc
// comment on why both a count cap and a byte cap are needed.
func TestHub_Publish_TooManyAddresseesRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	to := make([]string, MaxAddressees+1)
	for i := range to {
		to[i] = "a"
	}
	_, err := h.Publish("potato", "sess-1", to, "", "hi")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxAddressees)) {
		t.Errorf("error message %q does not name the %d-addressee limit", busErr.Msg, MaxAddressees)
	}
}

// TestHub_Publish_AddresseesTotalBytesOverLimitRejected proves the
// addressee cap is on combined raw length, not merely on count: two
// entries, both well under MaxAddressees, whose lengths sum past
// MaxAddresseesBytes must still be rejected.
func TestHub_Publish_AddresseesTotalBytesOverLimitRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	half := strings.Repeat("a", MaxAddresseesBytes/2+1)
	to := []string{half, half}
	_, err := h.Publish("potato", "sess-1", to, "", "hi")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, strconv.Itoa(MaxAddresseesBytes)) {
		t.Errorf("error message %q does not name the %d-byte limit", busErr.Msg, MaxAddresseesBytes)
	}
}

// --- Subscribe / fan-out ---

func TestHub_Subscribe_ReceivesEnvelopePublishedAfterSubscribing(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch)
	defer unsub()

	if _, err := h.Publish("potato", "sess-1", []string{"backend"}, "", "hello"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.Text != "hello" {
		t.Fatalf("received Text = %q, want %q", env.Text, "hello")
	}
}

// TestHub_Subscribe_TailNeverJoinsRoster locks in decision #5 from
// docs/design/atomic-bus.md: tail (and any other pure Subscribe caller)
// never appears in Who, and never claims a name.
func TestHub_Subscribe_TailNeverJoinsRoster(t *testing.T) {
	h := NewHub(t.TempDir())

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch)
	defer unsub()

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("expected no roster members from a bare Subscribe, got %d: %+v", len(members), members)
	}
}

func TestHub_Subscribe_UnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch)
	unsub()

	if _, err := h.Publish("potato", "sess-1", nil, "", "after unsubscribe"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case env := <-ch:
		t.Fatalf("received an envelope after unsubscribing: %+v", env)
	case <-time.After(200 * time.Millisecond):
		// expected: nothing arrives
	}
}

// TestHub_FanOut_DropMarkerPrecedesNextDeliveryAfterOverflow proves a
// dropped envelope is never indistinguishable from "nothing was sent":
// once a subscriber's buffer overflows, the next envelope that does fit
// is preceded by a synthetic control envelope naming the drop count and
// the room log path.
func TestHub_FanOut_DropMarkerPrecedesNextDeliveryAfterOverflow(t *testing.T) {
	home := t.TempDir()
	h := NewHub(home)
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A tiny buffer makes the overflow arithmetic exact and the test fast.
	ch := make(chan Envelope, 2)
	unsub := h.Subscribe("potato", ch)
	defer unsub()

	if _, err := h.Publish("potato", "sess-1", nil, "", "one"); err != nil {
		t.Fatalf("Publish one: %v", err)
	}
	if _, err := h.Publish("potato", "sess-1", nil, "", "two"); err != nil {
		t.Fatalf("Publish two: %v", err)
	}
	// The buffer is now full; both of these are dropped.
	if _, err := h.Publish("potato", "sess-1", nil, "", "dropped-1"); err != nil {
		t.Fatalf("Publish dropped-1: %v", err)
	}
	if _, err := h.Publish("potato", "sess-1", nil, "", "dropped-2"); err != nil {
		t.Fatalf("Publish dropped-2: %v", err)
	}

	// Drain what's buffered, freeing room for the marker and the next
	// real envelope.
	<-ch
	<-ch

	if _, err := h.Publish("potato", "sess-1", nil, "", "real"); err != nil {
		t.Fatalf("Publish real: %v", err)
	}

	marker := recvEnvelope(t, ch)
	if marker.From != "system" {
		t.Errorf("marker From = %q, want %q", marker.From, "system")
	}
	if !strings.Contains(marker.Text, "2") {
		t.Errorf("marker Text = %q, want it to name the drop count (2)", marker.Text)
	}
	if marker.Log != RoomLogPath(home, "potato") {
		t.Errorf("marker Log = %q, want %q", marker.Log, RoomLogPath(home, "potato"))
	}

	next := recvEnvelope(t, ch)
	if next.Text != "real" {
		t.Fatalf("expected the real envelope right after the marker, got %+v", next)
	}
}

// --- Since / ring replay ---

func TestHub_Since_ReplaysEverythingAfterGivenID(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	first, err := h.Publish("potato", "sess-1", nil, "", "one")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := h.Publish("potato", "sess-1", nil, "", "two"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := h.Publish("potato", "sess-1", nil, "", "three"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	envs, err := h.Since("potato", first.ID)
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("Since(%q) returned %d envelopes, want 2: %+v", first.ID, len(envs), envs)
	}
	if envs[0].Text != "two" || envs[1].Text != "three" {
		t.Fatalf("Since(%q) = %+v, want [two, three]", first.ID, envs)
	}
}

func TestHub_Since_EmptyCursorReturnsEverythingInRing(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	for _, text := range []string{"one", "two"} {
		if _, err := h.Publish("potato", "sess-1", nil, "", text); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	envs, err := h.Since("potato", "")
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(envs) != 2 {
		t.Fatalf("Since(\"\") returned %d envelopes, want 2", len(envs))
	}
}

func TestHub_Since_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Since("nonexistent", "")
	mustError(t, err, ExitNoRoom)
}

// TestHub_Since_ToleratesAnEvictedID publishes past the ring's capacity so
// the id it asks Since for has fallen out of the 256-entry window, and
// asserts that is not an error — Since instead returns whatever remains,
// per docs/spec/atomic-bus.md's Since row ("an id no longer in the ring is
// not an error").
func TestHub_Since_ToleratesAnEvictedID(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	first, err := h.Publish("potato", "sess-1", nil, "", "evicted")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Push well past ringCapacity so `first` is guaranteed evicted.
	var lastText string
	for i := 0; i < ringCapacity+10; i++ {
		lastText = "msg-" + string(rune('a'+(i%26)))
		if _, err := h.Publish("potato", "sess-1", nil, "", lastText); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	envs, err := h.Since("potato", first.ID)
	if err != nil {
		t.Fatalf("Since with an evicted id must not error, got: %v", err)
	}
	if len(envs) != ringCapacity {
		t.Fatalf("Since with an evicted id returned %d envelopes, want the full ring (%d)", len(envs), ringCapacity)
	}
	if envs[len(envs)-1].Text != lastText {
		t.Fatalf("last envelope = %q, want the most recently published %q", envs[len(envs)-1].Text, lastText)
	}
}

// --- Halt / Resume: server-enforced, not advisory ---

// TestHub_Halt_BlocksAgentPublish_ButNotHumanPublish is the asymmetry the
// whole halt feature exists for: an agent's send must be rejected, and a
// human's send must not be, because that asymmetry is what lets an
// operator still speak into (and thereby direct) a room they've stopped.
func TestHub_Halt_BlocksAgentPublish_ButNotHumanPublish(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent"); err != nil {
		t.Fatalf("Join agent: %v", err)
	}
	if _, err := h.Join("potato", "operator", "normal", "human", "sess-human"); err != nil {
		t.Fatalf("Join human: %v", err)
	}

	if err := h.Halt("potato", "stop, wrong approach"); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	halted, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if !halted {
		t.Fatal("expected room to be halted")
	}

	if _, err := h.Publish("potato", "sess-agent", nil, "", "i will keep going"); err == nil {
		t.Fatal("expected agent Publish into a halted room to fail")
	} else {
		mustError(t, err, ExitHalted)
	}

	if _, err := h.Publish("potato", "sess-human", nil, "", "hold on"); err != nil {
		t.Fatalf("expected human Publish into a halted room to succeed, got: %v", err)
	}
}

// TestHub_Resume_RestoresAgentPublish proves resume is not merely "halt
// wears off" but actually flips the enforced flag back.
func TestHub_Resume_RestoresAgentPublish(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Halt("potato", "stop"); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	if err := h.Resume("potato", "go ahead"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	halted, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if halted {
		t.Fatal("expected room to no longer be halted after Resume")
	}

	if _, err := h.Publish("potato", "sess-agent", nil, "", "resumed"); err != nil {
		t.Fatalf("expected agent Publish to succeed after Resume, got: %v", err)
	}
}

// TestHub_Halt_AppendFailureDoesNotFlipHaltedFlag reproduces the
// checkpoint 2 review finding that setHalted flipped r.halted before the
// durable append: on an Append failure the operator's error implied the
// halt might not have taken effect, while the room was in fact halted
// with no control envelope ever logged or broadcast to prove it. Forces
// the failure deterministically by making the room log's parent directory
// unwritable, mirroring internal/repoinit's
// TestInit_MkdirErrorPropagates.
func TestHub_Halt_AppendFailureDoesNotFlipHaltedFlag(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.Mkdir(atomicDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(atomicDir, 0o755) })

	h := NewHub(home)
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Halt("potato", "stop"); err == nil {
		t.Fatal("expected Halt to fail when the room log append fails")
	}

	halted, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if halted {
		t.Fatal("halted flag flipped even though the durable append failed")
	}
}

func TestHub_Halt_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	err := h.Halt("nonexistent", "stop")
	mustError(t, err, ExitNoRoom)
}

// TestHub_Halt_PublishesControlEnvelopeVisibleToSubscribers proves halt
// binds by being observable, not just by rejecting the next send — a
// watching subscriber (e.g. `atomic bus tail`) sees the halt announcement
// itself as a normal envelope.
func TestHub_Halt_PublishesControlEnvelopeVisibleToSubscribers(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent"); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch)
	defer unsub()

	if err := h.Halt("potato", "stop, wrong approach"); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.FromKind != "human" {
		t.Errorf("halt control envelope FromKind = %q, want %q", env.FromKind, "human")
	}
	if env.Text != "stop, wrong approach" {
		t.Errorf("halt control envelope Text = %q, want %q", env.Text, "stop, wrong approach")
	}
}

// --- roomlog.go: Append / ReadSince ---
//
// No dedicated roomlog_test.go exists in this checkpoint's file scope
// (see the atomic-bus brief); Append and ReadSince are exercised directly
// here rather than only indirectly through Hub.Publish.

func TestAppend_ReadSince_RoundTrip(t *testing.T) {
	home := t.TempDir()

	env1 := Envelope{ID: "1", Room: "potato", From: "frontend", FromKind: "agent", Text: "one", Ts: time.Now()}
	env2 := Envelope{ID: "2", Room: "potato", From: "frontend", FromKind: "agent", Text: "two", Ts: time.Now()}

	if err := Append(home, "potato", env1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(home, "potato", env2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, err := ReadSince(home, "potato", "")
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ReadSince(\"\") = %d envelopes, want 2", len(all))
	}

	afterFirst, err := ReadSince(home, "potato", "1")
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(afterFirst) != 1 || afterFirst[0].Text != "two" {
		t.Fatalf("ReadSince(%q) = %+v, want [two]", "1", afterFirst)
	}
}

// TestAppend_ReadSince_MessageAtMaxTextBytesRoundTrips is the other half
// of finding 7's fix: a message right at the limit Publish admits must
// always read back — the scanner buffer has to clear MaxTextBytes by
// enough margin to cover the envelope's JSON wrapper and escaping
// overhead, not just equal it.
func TestAppend_ReadSince_MessageAtMaxTextBytesRoundTrips(t *testing.T) {
	home := t.TempDir()
	text := strings.Repeat("a", MaxTextBytes)
	env := Envelope{ID: "1", Room: "potato", From: "frontend", FromKind: "agent", Text: text, Ts: time.Now()}

	if err := Append(home, "potato", env); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, err := ReadSince(home, "potato", "")
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ReadSince returned %d envelopes, want 1", len(all))
	}
	if len(all[0].Text) != MaxTextBytes {
		t.Fatalf("round-tripped Text length = %d, want %d", len(all[0].Text), MaxTextBytes)
	}
}

// TestAppend_ReadSince_EnvelopeAtEveryMetadataLimitRoundTrips is finding 2's
// full regression, and the actual proof behind scannerMaxLineBytes's
// arithmetic comment: every capped field (Room, From, ReplyTo at
// MaxIdentifierBytes; To with MaxAddressees entries whose combined length
// is exactly MaxAddresseesBytes) simultaneously at its admitted limit,
// alongside Text at MaxTextBytes, must still round-trip through
// Append/ReadSince — not merely Text alone, which
// TestAppend_ReadSince_MessageAtMaxTextBytesRoundTrips above already
// covers.
//
// Every capped field is filled with 0x01 rather than a plain letter: a
// plain ASCII byte marshals to itself, so a same-length fill only proves
// maximum *length* round-trips. 0x01 has no short escape in encoding/json
// (unlike \n, \t, ...), so it marshals to the full 6-byte \u0001
// escape sequence — this is what actually exercises the worst-case
// *escaped* size scannerMaxLineBytes's arithmetic is derived from.
func TestAppend_ReadSince_EnvelopeAtEveryMetadataLimitRoundTrips(t *testing.T) {
	home := t.TempDir()

	const escaping = "\x01"
	room := strings.Repeat(escaping, MaxIdentifierBytes)
	from := strings.Repeat(escaping, MaxIdentifierBytes)
	replyTo := strings.Repeat(escaping, MaxIdentifierBytes)
	text := strings.Repeat(escaping, MaxTextBytes)

	to := make([]string, MaxAddressees)
	addresseeLen := MaxAddresseesBytes / MaxAddressees
	for i := range to {
		to[i] = strings.Repeat(escaping, addresseeLen)
	}

	env := Envelope{
		ID: strconv.FormatUint(^uint64(0), 36), Room: room, From: from, FromKind: "agent",
		To: to, ReplyTo: replyTo, Ts: time.Now(), Text: text,
	}

	// The worst-case envelope must actually fit inside scannerMaxLineBytes
	// with room to spare — not merely happen to round-trip through this
	// particular filesystem/scanner combination. This is the assertion a
	// too-small scanner budget fails first.
	marshaled, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if len(marshaled) > scannerMaxLineBytes {
		t.Fatalf("worst-case envelope marshals to %d bytes, exceeds scannerMaxLineBytes = %d", len(marshaled), scannerMaxLineBytes)
	}

	if err := Append(home, room, env); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all, err := ReadSince(home, room, "")
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ReadSince returned %d envelopes, want 1", len(all))
	}

	got := all[0]
	if got.ID != env.ID {
		t.Errorf("round-tripped ID = %q, want %q", got.ID, env.ID)
	}
	if got.Room != room {
		t.Errorf("round-tripped Room length = %d, want %d", len(got.Room), MaxIdentifierBytes)
	}
	if got.From != from {
		t.Errorf("round-tripped From length = %d, want %d", len(got.From), MaxIdentifierBytes)
	}
	if got.ReplyTo != replyTo {
		t.Errorf("round-tripped ReplyTo length = %d, want %d", len(got.ReplyTo), MaxIdentifierBytes)
	}
	if len(got.To) != MaxAddressees {
		t.Fatalf("round-tripped To has %d entries, want %d", len(got.To), MaxAddressees)
	}
	for i, addr := range got.To {
		if addr != to[i] {
			t.Errorf("round-tripped To[%d] length = %d, want %d", i, len(addr), addresseeLen)
		}
	}
	if len(got.Text) != MaxTextBytes {
		t.Fatalf("round-tripped Text length = %d, want %d", len(got.Text), MaxTextBytes)
	}
}

func TestReadSince_MissingLogFileIsNotAnError(t *testing.T) {
	home := t.TempDir()

	envs, err := ReadSince(home, "nevertouched", "")
	if err != nil {
		t.Fatalf("ReadSince on a room with no log file should not error, got: %v", err)
	}
	if len(envs) != 0 {
		t.Fatalf("expected no envelopes, got %d", len(envs))
	}
}

// TestAppend_ConcurrentAppendsAllLandWithoutCorruption is roomlog.go's own
// concurrency property ("Appends must be safe against concurrent
// publishes" per the brief) proven independently of Hub.mu, which already
// serializes every real Publish call — this test calls Append directly
// and concurrently to prove the file-level safety holds on its own too.
func TestAppend_ConcurrentAppendsAllLandWithoutCorruption(t *testing.T) {
	home := t.TempDir()
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env := Envelope{ID: strconv.Itoa(i), Room: "potato", From: "frontend", FromKind: "agent", Text: "concurrent", Ts: time.Now()}
			if err := Append(home, "potato", env); err != nil {
				t.Errorf("Append %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	envs, err := ReadSince(home, "potato", "")
	if err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	if len(envs) != n {
		t.Fatalf("ReadSince returned %d envelopes, want %d (a corrupted/interleaved write would drop or mangle lines)", len(envs), n)
	}
}

// --- paths sanity (belt-and-suspenders on top of paths_test's own coverage) ---

func TestRoomLogPath_MatchesHubHome(t *testing.T) {
	home := t.TempDir()
	got := RoomLogPath(home, "potato")
	want := filepath.Join(home, ".atomic", "rooms", "potato.log")
	if got != want {
		t.Fatalf("RoomLogPath = %q, want %q", got, want)
	}
}
