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

	name, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", "")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if name != "backend" {
		t.Fatalf("name = %q, want %q", name, "backend")
	}
}

// TestHub_Join_StoresRepoAndRealmOnMember is the position-derived naming
// entry's core Hub-level assertion (docs/spec/atomic-bus.md, 2026-07-29):
// Join stores whatever repo/realm the caller reports directly on the
// roster — Member is the record of a resolved position, not merely a name.
func TestHub_Join_StoresRepoAndRealmOnMember(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "atomic-claude", "myrealm"); err != nil {
		t.Fatalf("Join: %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members = %+v, want 1", members)
	}
	if members[0].Repo != "atomic-claude" {
		t.Errorf("Repo = %q, want %q", members[0].Repo, "atomic-claude")
	}
	if members[0].Realm != "myrealm" {
		t.Errorf("Realm = %q, want %q", members[0].Realm, "myrealm")
	}
}

// TestHub_Join_EmptyRealmIsValidNotFabricated proves an empty realm at
// Join stays empty on the roster — never a placeholder — per the spec's
// "both empty is valid" criterion.
func TestHub_Join_EmptyRealmIsValidNotFabricated(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "atomic-claude", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || members[0].Realm != "" {
		t.Fatalf("members = %+v, want one member with empty Realm", members)
	}
}

func TestHub_Join_SecondClaimOfSameNameGetsNumericSuffix(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("first Join: %v", err)
	}

	name, err := h.Join("potato", "backend", "normal", "agent", "sess-2", "", "")
	if err != nil {
		t.Fatalf("second Join: %v", err)
	}
	if name != "backend-2" {
		t.Fatalf("name = %q, want %q", name, "backend-2")
	}
}

func TestHub_Join_ThirdClaimOfSameNameFailsWithNameTaken(t *testing.T) {
	h := NewHub(t.TempDir())

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("second Join: %v", err)
	}

	_, err := h.Join("potato", "backend", "normal", "agent", "sess-3", "", "")
	mustError(t, err, ExitNameTaken)
}

func TestHub_Join_SameNameDifferentRoomsDoNotCollide(t *testing.T) {
	h := NewHub(t.TempDir())

	name1, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", "")
	if err != nil {
		t.Fatalf("Join room potato: %v", err)
	}
	name2, err := h.Join("carrot", "backend", "normal", "agent", "sess-2", "", "")
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

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("first Join: %v", err)
	}
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
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

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join backend: %v", err)
	}
	if _, err := h.Join("potato", "worker", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join worker: %v", err)
	}
	if _, err := h.Join("potato", "worker", "normal", "agent", "sess-3", "", ""); err != nil {
		t.Fatalf("Join worker-2: %v", err)
	}

	// sess-1 attempts to rejoin as "worker", which is taken in both its
	// bare and "-2" forms by sess-2 and sess-3.
	_, err := h.Join("potato", "worker", "normal", "agent", "sess-1", "", "")
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
			name, err := h.Join("potato", "backend", "normal", "agent", session, "", "")
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
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join backend: %v", err)
	}

	_, err := h.Join("potato", "system", "normal", "agent", "sess-2", "", "")
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
	_, err := h.Join("potato", "backend", "normal", "hologram", "sess-1", "", "")
	mustError(t, err, ExitUsage)

	if rooms := h.Rooms(); len(rooms) != 0 {
		t.Fatalf("expected no room created by a rejected Join, got %v", rooms)
	}
}

// TestHub_Join_OverLongRoomNameRejected is finding 2's Join-side half: an
// unbounded room name written into every envelope's Room field could grow
// the room log without bound, the same failure class MaxTextBytes closed
// for Text.
func TestHub_Join_OverLongRoomNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	overlong := strings.Repeat("r", MaxIdentifierBytes+1)

	_, err := h.Join(overlong, "backend", "normal", "agent", "sess-1", "", "")
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

	_, err := h.Join("potato", overlong, "normal", "agent", "sess-1", "", "")
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

// TestHub_Rehydrate_RestoresRepoAndRealm proves repo/realm survive a
// daemon restart via bus.json rehydration, exactly like mode/kind
// (docs/spec/atomic-bus.md: "mode, kind, repo, and realm all survive a
// daemon restart via bus.json rehydration").
func TestHub_Rehydrate_RestoresRepoAndRealm(t *testing.T) {
	h := NewHub(t.TempDir())
	st := &State{Sessions: map[string]*sessionState{
		"sess-1": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: time.Now(), Repo: "atomic-claude", Realm: "myrealm"},
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
	if members[0].Repo != "atomic-claude" {
		t.Errorf("Repo = %q, want %q", members[0].Repo, "atomic-claude")
	}
	if members[0].Realm != "myrealm" {
		t.Errorf("Realm = %q, want %q", members[0].Realm, "myrealm")
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
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	unknown := h.UnknownAddressees("potato", []string{"backend", "nobody-here"})
	if len(unknown) != 1 || unknown[0] != "nobody-here" {
		t.Fatalf("UnknownAddressees = %v, want [nobody-here]", unknown)
	}
}

func TestHub_UnknownAddressees_AllKnownReturnsEmpty(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
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

// --- --to resolution: exact match, then unique suffix/substring
// (docs/spec/atomic-bus.md's 2026-07-29 "the name is the position; --as is
// the role" entry) ---

func TestRoom_ResolveOneAddressee_ExactMatchWinsOverSuffixCollision(t *testing.T) {
	r := &Room{members: map[string]Member{
		"taxgentic-gui-fe-main":   {Name: "taxgentic-gui-fe-main"},
		"taxgentic-gui-fe-main-2": {Name: "taxgentic-gui-fe-main-2"},
	}}
	got, err := r.resolveOneAddressee("taxgentic-gui-fe-main")
	if err != nil {
		t.Fatalf("resolveOneAddressee: %v", err)
	}
	if got != "taxgentic-gui-fe-main" {
		t.Fatalf("got %q, want the exact member, never the -2 collision sibling", got)
	}
}

func TestRoom_ResolveOneAddressee_UniqueSubstringResolves(t *testing.T) {
	r := &Room{members: map[string]Member{
		"taxgentic-gui-fe-main": {Name: "taxgentic-gui-fe-main"},
		"noorm-monorepo-be":     {Name: "noorm-monorepo-be"},
	}}
	got, err := r.resolveOneAddressee("fe-main")
	if err != nil {
		t.Fatalf("resolveOneAddressee: %v", err)
	}
	if got != "taxgentic-gui-fe-main" {
		t.Fatalf("got %q, want %q", got, "taxgentic-gui-fe-main")
	}
}

// TestRoom_ResolveOneAddressee_UniqueSuffixResolves proves the suffix case
// separately from the substring case above: a suffix is a substring that
// happens to end the string, so a single strings.Contains scan already
// covers both — this pins that a change narrowing the match to
// strings.HasSuffix alone (which would still pass the substring test above,
// since "fe-main" is also a suffix there) cannot silently ship, since a
// pure mid-string match ("gui" below) would then stop resolving.
func TestRoom_ResolveOneAddressee_UniqueSuffixResolves(t *testing.T) {
	r := &Room{members: map[string]Member{
		"taxgentic-gui-fe-main": {Name: "taxgentic-gui-fe-main"},
	}}
	got, err := r.resolveOneAddressee("main")
	if err != nil {
		t.Fatalf("resolveOneAddressee: %v", err)
	}
	if got != "taxgentic-gui-fe-main" {
		t.Fatalf("got %q, want %q", got, "taxgentic-gui-fe-main")
	}
}

func TestRoom_ResolveOneAddressee_MidStringSubstring_AlsoResolves(t *testing.T) {
	r := &Room{members: map[string]Member{
		"taxgentic-gui-fe-main": {Name: "taxgentic-gui-fe-main"},
	}}
	got, err := r.resolveOneAddressee("gui")
	if err != nil {
		t.Fatalf("resolveOneAddressee: %v", err)
	}
	if got != "taxgentic-gui-fe-main" {
		t.Fatalf("got %q, want %q", got, "taxgentic-gui-fe-main")
	}
}

// TestRoom_ResolveOneAddressee_AmbiguousMatch_ErrorsNamingEveryCandidate
// proves ambiguity is an error, never a silent pick, and that the error is
// distinguishable from Hub.UnknownAddressees's "not currently in room"
// warning text — the two mean different things and must never be confused.
func TestRoom_ResolveOneAddressee_AmbiguousMatch_ErrorsNamingEveryCandidate(t *testing.T) {
	r := &Room{members: map[string]Member{
		"taxgentic-gui-fe-main": {Name: "taxgentic-gui-fe-main"},
		"taxgentic-api-fe-main": {Name: "taxgentic-api-fe-main"},
	}}
	_, err := r.resolveOneAddressee("fe-main")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, "taxgentic-gui-fe-main") || !strings.Contains(busErr.Msg, "taxgentic-api-fe-main") {
		t.Fatalf("error message %q does not name every candidate", busErr.Msg)
	}
	if strings.Contains(busErr.Msg, "not currently in room") {
		t.Fatalf("error message %q reuses the unrelated \"not currently in room\" warning text", busErr.Msg)
	}
}

func TestRoom_ResolveOneAddressee_NoMatch_PassesThroughUnresolved(t *testing.T) {
	r := &Room{members: map[string]Member{"backend": {Name: "backend"}}}
	got, err := r.resolveOneAddressee("nobody-here")
	if err != nil {
		t.Fatalf("resolveOneAddressee: %v", err)
	}
	if got != "nobody-here" {
		t.Fatalf("got %q, want the literal unresolved name — Hub.UnknownAddressees warns on this, it is not an ambiguity error", got)
	}
}

func TestRoom_ResolveAddressees_EmptyToStaysNil(t *testing.T) {
	r := &Room{members: map[string]Member{"backend": {Name: "backend"}}}
	got, err := r.resolveAddressees(nil)
	if err != nil {
		t.Fatalf("resolveAddressees: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("resolveAddressees(nil) = %v, want empty (an FYI message must stay unaddressed)", got)
	}
}

func TestHub_Publish_ToExactMatch_WinsOverSuffixCollision(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "taxgentic-gui-fe-main", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "taxgentic-gui-fe-main", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join (collision): %v", err)
	}
	if _, err := h.Join("potato", "sender", "normal", "agent", "sess-3", "", ""); err != nil {
		t.Fatalf("Join sender: %v", err)
	}

	env, err := h.Publish("potato", "sess-3", []string{"taxgentic-gui-fe-main"}, "", "hi")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(env.To) != 1 || env.To[0] != "taxgentic-gui-fe-main" {
		t.Fatalf("To = %v, want exactly [taxgentic-gui-fe-main], never the -2 sibling", env.To)
	}
}

func TestHub_Publish_ToResolvesUniqueSubstring_DeliveredUnderFullName(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "taxgentic-gui-fe-main", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "sender", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join sender: %v", err)
	}

	env, err := h.Publish("potato", "sess-2", []string{"fe-main"}, "", "hi")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(env.To) != 1 || env.To[0] != "taxgentic-gui-fe-main" {
		t.Fatalf("To = %v, want [taxgentic-gui-fe-main]", env.To)
	}
}

// TestHub_Publish_ToAmbiguous_AbortsSend_NoEnvelopeAppended proves an
// ambiguous --to aborts the whole send rather than delivering under a
// half-resolved list: no envelope reaches the room log at all.
func TestHub_Publish_ToAmbiguous_AbortsSend_NoEnvelopeAppended(t *testing.T) {
	home := t.TempDir()
	h := NewHub(home)
	if _, err := h.Join("potato", "taxgentic-gui-fe-main", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "taxgentic-api-fe-main", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "sender", "normal", "agent", "sess-3", "", ""); err != nil {
		t.Fatalf("Join sender: %v", err)
	}

	_, err := h.Publish("potato", "sess-3", []string{"fe-main"}, "", "hi")
	busErr := mustError(t, err, ExitUsage)
	if !strings.Contains(busErr.Msg, "taxgentic-gui-fe-main") || !strings.Contains(busErr.Msg, "taxgentic-api-fe-main") {
		t.Fatalf("error %q does not name both candidates", busErr.Msg)
	}
	if _, statErr := os.Stat(RoomLogPath(home, "potato")); !os.IsNotExist(statErr) {
		t.Fatalf("room log exists after an ambiguous --to aborted the send; want nothing appended")
	}
}

func TestHub_PublishAsOperator_ToResolvesUniqueSubstring(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "taxgentic-gui-fe-main", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	env, err := h.PublishAsOperator("potato", []string{"fe-main"}, "", "hi")
	if err != nil {
		t.Fatalf("PublishAsOperator: %v", err)
	}
	if len(env.To) != 1 || env.To[0] != "taxgentic-gui-fe-main" {
		t.Fatalf("To = %v, want [taxgentic-gui-fe-main]", env.To)
	}
}

// --- Leave / Who / Rooms ---

func TestHub_Leave_RemovesMemberFromRoster(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	// A second member keeps the room non-empty after sess-1 leaves, so this
	// test proves member removal specifically, independent of the
	// auto-drop-when-empty behavior covered by its own tests below.
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join second member: %v", err)
	}

	if _, err := h.Leave("potato", "sess-1"); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || members[0].Name != "frontend" {
		t.Fatalf("members after Leave = %+v, want only frontend remaining", members)
	}
}

func TestHub_Leave_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Leave("nonexistent", "sess-1")
	mustError(t, err, ExitNoRoom)
}

func TestHub_Leave_SessionNotMemberReturnsExitNotJoined(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	_, err := h.Leave("potato", "sess-stranger")
	mustError(t, err, ExitNotJoined)
}

// TestHub_Leave_LastMemberDropsTheRoom is the auto-drop half of
// docs/spec/atomic-bus.md's 2026-07-30 "drop a room when its last member
// leaves" entry: a room created by a typo (or simply finished with) does
// not linger forever with zero members.
func TestHub_Leave_LastMemberDropsTheRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	dropped, err := h.Leave("potato", "sess-1")
	if err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if !dropped {
		t.Fatal("expected Leave to report the room as dropped")
	}

	if _, err := h.Who("potato"); err == nil {
		t.Fatal("expected the room to no longer exist after its last member left")
	} else {
		mustError(t, err, ExitNoRoom)
	}
	if got := h.Rooms(); len(got) != 0 {
		t.Fatalf("Rooms() = %+v, want no rooms (potato should have been dropped, not merely emptied)", got)
	}
}

// TestHub_Leave_LastMemberWithLiveSubscriberDoesNotDropTheRoom is the
// guard clause on the auto-drop above: a tail or recv holding an open
// subscription on an otherwise-empty room must not be orphaned — dropping
// the room would mean any future Publish creates a brand new Room object
// with an empty subs map, and this subscriber would never hear anything
// again with no error to explain why.
func TestHub_Leave_LastMemberWithLiveSubscriberDoesNotDropTheRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	dropped, err := h.Leave("potato", "sess-1")
	if err != nil {
		t.Fatalf("Leave: %v", err)
	}
	if dropped {
		t.Fatal("expected Leave to report the room as NOT dropped while a subscriber is attached")
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v (the room should still exist)", err)
	}
	if len(members) != 0 {
		t.Fatalf("members = %+v, want none (the last member did leave)", members)
	}

	env, err := h.Publish("potato", "", nil, "", "still listening")
	// A memberless room has no member to publish as via Publish (session
	// unmatched) — PublishAsOperator is the right path here, mirroring how
	// `say` reaches a room with no roster entry of its own.
	_ = env
	if err == nil {
		t.Fatal("Publish with no session should still fail with ExitNotJoined — using PublishAsOperator below to actually prove delivery")
	}
	if _, err := h.PublishAsOperator("potato", nil, "", "still listening"); err != nil {
		t.Fatalf("PublishAsOperator: %v (the room must still exist to publish into)", err)
	}
	select {
	case got := <-ch:
		if got.Text != "still listening" {
			t.Fatalf("delivered envelope text = %q, want %q", got.Text, "still listening")
		}
	default:
		t.Fatal("subscriber received nothing — the room was orphaned instead of kept alive")
	}
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
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join potato: %v", err)
	}
	if _, err := h.Join("carrot", "backend", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join carrot: %v", err)
	}
	if _, err := h.Join("carrot", "frontend", "normal", "agent", "sess-3", "", ""); err != nil {
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

// TestHub_Rooms_EmptyRoomWithLiveSubscriberStillListedWithZeroMembers is the
// surviving case of the old "rooms persist after everyone leaves" contract:
// dropIfEmpty only drops a room with *neither* members *nor* subscribers, so
// a room a tail is still watching keeps appearing in Rooms() with an honest
// Members == 0 even though its last member has left
// (docs/spec/atomic-bus.md's 2026-07-30 "drop a room when its last member
// leaves — but not while a tail or recv is attached" clause).
func TestHub_Rooms_EmptyRoomWithLiveSubscriberStillListedWithZeroMembers(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	if _, err := h.Leave("potato", "sess-1"); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h1.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join (daemon 1): %v", err)
	}
	env1, err := h1.Publish("potato", "sess-1", nil, "", "before restart")
	if err != nil {
		t.Fatalf("Publish (daemon 1): %v", err)
	}

	// A fresh Hub against the same home, exactly what a respawned daemon
	// constructs — its roster and id bookkeeping start over from nothing.
	h2 := NewHub(home)
	if _, err := h2.Join("potato", "frontend", "normal", "agent", "sess-2", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A subscriber channel nobody ever reads from.
	deadCh := make(chan Envelope) // unbuffered on purpose: any blocking
	// send here would hang forever without the non-blocking fanOut.
	unsub := h.Subscribe("potato", deadCh, "", false)
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
// log — a bound on how large a single room-log line can grow.
func TestHub_Publish_OversizedTextRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	if _, err := h.Publish("potato", "sess-1", []string{"backend"}, "", "hello"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.Text != "hello" {
		t.Fatalf("received Text = %q, want %q", env.Text, "hello")
	}
}

// TestHub_Subscribe_PriorTrafficNotDelivered_OnlyFuturePublishesArrive is
// the Hub-level proof of the bug this change fixes: a room with existing
// traffic must not replay any of it to a newly subscribing recv — a
// subscriber sees only what is published after it subscribes
// (docs/spec/atomic-bus.md: "Non-goals: Replay of any kind"). The prior
// ring-backed Since("") returned the entire ring, so a recv on a busy room
// delivered old messages as Monitor notifications, each acted on as if
// freshly arrived.
func TestHub_Subscribe_PriorTrafficNotDelivered_OnlyFuturePublishesArrive(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Traffic published before anyone subscribes.
	if _, err := h.Publish("potato", "sess-1", nil, "", "before subscribing"); err != nil {
		t.Fatalf("Publish before: %v", err)
	}

	ch := make(chan Envelope, 4)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	select {
	case env := <-ch:
		t.Fatalf("received an envelope published before subscribing: %+v", env)
	case <-time.After(200 * time.Millisecond):
		// expected: nothing delivered yet
	}

	if _, err := h.Publish("potato", "sess-1", nil, "", "after subscribing"); err != nil {
		t.Fatalf("Publish after: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.Text != "after subscribing" {
		t.Fatalf("received Text = %q, want %q (only post-subscribe traffic should ever arrive)", env.Text, "after subscribing")
	}
}

// TestHub_Subscribe_TailNeverJoinsRoster locks in decision #5 from
// docs/design/atomic-bus.md: tail (and any other pure Subscribe caller)
// never appears in Who, and never claims a name.
func TestHub_Subscribe_TailNeverJoinsRoster(t *testing.T) {
	h := NewHub(t.TempDir())

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
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
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// A tiny buffer makes the overflow arithmetic exact and the test fast.
	ch := make(chan Envelope, 2)
	unsub := h.Subscribe("potato", ch, "", false)
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

// --- Halt / Resume: server-enforced, not advisory ---

// TestHub_Halt_BlocksAgentPublish_ButNotHumanPublish is the asymmetry the
// whole halt feature exists for: an agent's send must be rejected, and a
// human's send must not be, because that asymmetry is what lets an
// operator still speak into (and thereby direct) a room they've stopped.
func TestHub_Halt_BlocksAgentPublish_ButNotHumanPublish(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join agent: %v", err)
	}
	if _, err := h.Join("potato", "operator", "normal", "human", "sess-human", "", ""); err != nil {
		t.Fatalf("Join human: %v", err)
	}

	if err := h.Halt("potato", "stop, wrong approach"); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	halted, _, err := h.IsHalted("potato")
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
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Halt("potato", "stop"); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	if err := h.Resume("potato", "go ahead"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	halted, _, err := h.IsHalted("potato")
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
	atomicDir := filepath.Join(home, ".atomic", "", "")
	if err := os.Mkdir(atomicDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(atomicDir, 0o755) })

	h := NewHub(home)
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Halt("potato", "stop"); err == nil {
		t.Fatal("expected Halt to fail when the room log append fails")
	}

	halted, _, err := h.IsHalted("potato")
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
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
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

// --- roomlog.go: Append ---
//
// No dedicated roomlog_test.go exists in this checkpoint's file scope (see
// the atomic-bus brief); Append is exercised directly here rather than
// only indirectly through Hub.Publish. Reads go straight at the on-disk
// JSONL file rather than through a package API — roomlog.go no longer
// exposes a read path (ReadSince backed --since replay, now removed); the
// room log is the durable record, not something the daemon reads back.

// readRoomLog reads every envelope in room's on-disk log, in append order.
// A missing log file yields an empty, nil-error result — mirrors Append's
// own "room has never had a message" case.
func readRoomLog(t *testing.T, home, room string) []Envelope {
	t.Helper()
	b, err := os.ReadFile(RoomLogPath(home, room))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read room log: %v", err)
	}
	var all []Envelope
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line == "" {
			continue
		}
		var env Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("unmarshal room log line %q: %v", line, err)
		}
		all = append(all, env)
	}
	return all
}

func TestAppend_RoundTrip(t *testing.T) {
	home := t.TempDir()

	env1 := Envelope{ID: "1", Room: "potato", From: "frontend", FromKind: "agent", Text: "one", Ts: time.Now()}
	env2 := Envelope{ID: "2", Room: "potato", From: "frontend", FromKind: "agent", Text: "two", Ts: time.Now()}

	if err := Append(home, "potato", env1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(home, "potato", env2); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all := readRoomLog(t, home, "potato")
	if len(all) != 2 {
		t.Fatalf("room log has %d envelopes, want 2", len(all))
	}
	if all[0].Text != "one" || all[1].Text != "two" {
		t.Fatalf("room log = %+v, want [one, two] in append order", all)
	}
}

// TestAppend_MessageAtMaxTextBytesRoundTrips is the other half of finding
// 7's fix: a message right at the limit Publish admits must always read
// back intact.
func TestAppend_MessageAtMaxTextBytesRoundTrips(t *testing.T) {
	home := t.TempDir()
	text := strings.Repeat("a", MaxTextBytes)
	env := Envelope{ID: "1", Room: "potato", From: "frontend", FromKind: "agent", Text: text, Ts: time.Now()}

	if err := Append(home, "potato", env); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all := readRoomLog(t, home, "potato")
	if len(all) != 1 {
		t.Fatalf("room log has %d envelopes, want 1", len(all))
	}
	if len(all[0].Text) != MaxTextBytes {
		t.Fatalf("round-tripped Text length = %d, want %d", len(all[0].Text), MaxTextBytes)
	}
}

// TestAppend_EnvelopeAtEveryMetadataLimitRoundTrips is finding 2's full
// regression: every capped field (Room, From, ReplyTo at
// MaxIdentifierBytes; To with MaxAddressees entries whose combined length
// is exactly MaxAddresseesBytes) simultaneously at its admitted limit,
// alongside Text at MaxTextBytes, must still round-trip through Append —
// not merely Text alone, which TestAppend_MessageAtMaxTextBytesRoundTrips
// above already covers.
//
// Every capped field is filled with 0x01 rather than a plain letter: a
// plain ASCII byte marshals to itself, so a same-length fill only proves
// maximum *length* round-trips. 0x01 has no short escape in encoding/json
// (unlike \n, \t, ...), so it marshals to the full 6-byte \u0001 escape
// sequence — the worst-case *escaped* size a Publish-admitted envelope
// can reach on disk.
func TestAppend_EnvelopeAtEveryMetadataLimitRoundTrips(t *testing.T) {
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

	if err := Append(home, room, env); err != nil {
		t.Fatalf("Append: %v", err)
	}

	all := readRoomLog(t, home, room)
	if len(all) != 1 {
		t.Fatalf("room log has %d envelopes, want 1", len(all))
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

func TestReadRoomLog_MissingLogFileIsNotAnError(t *testing.T) {
	home := t.TempDir()

	envs := readRoomLog(t, home, "nevertouched")
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

	envs := readRoomLog(t, home, "potato")
	if len(envs) != n {
		t.Fatalf("room log has %d envelopes, want %d (a corrupted/interleaved write would drop or mangle lines)", len(envs), n)
	}
}

// --- paths sanity (belt-and-suspenders on top of paths_test's own coverage) ---

func TestRoomLogPath_MatchesHubHome(t *testing.T) {
	home := t.TempDir()
	got := RoomLogPath(home, "potato")
	want := filepath.Join(home, ".atomic", "rooms", "potato.log", "", "")
	if got != want {
		t.Fatalf("RoomLogPath = %q, want %q", got, want)
	}
}

// --- PublishAsOperator: say's path, publishing without a roster entry ---
//
// docs/spec/atomic-bus.md "say — one-shot send without joining."
//
// The sender identity is not a parameter. An earlier signature took name and
// kind from the caller and was reachable from the wire via OpSay, which let a
// raw request claim an existing agent's name with kind "agent" and publish into
// a halted room. These tests pin the properties that fix relies on.

// TestHub_PublishAsOperator_SucceedsWithoutPriorJoin_NoRosterMemberAdded proves
// the defining property: an envelope lands and the roster is untouched — say
// never occupies a name (mirroring tail's own no-roster-footprint guarantee in
// TestHub_Subscribe_TailNeverJoinsRoster above).
func TestHub_PublishAsOperator_SucceedsWithoutPriorJoin_NoRosterMemberAdded(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	env, err := h.PublishAsOperator("potato", nil, "", "operator speaking")
	if err != nil {
		t.Fatalf("PublishAsOperator: %v", err)
	}
	if env.From != operatorName || env.FromKind != KindHuman {
		t.Fatalf("From/FromKind = %q/%q, want %q/%q", env.From, env.FromKind, operatorName, KindHuman)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || members[0].Name != "backend" {
		t.Fatalf("members = %+v, want only backend (say must not add a roster entry)", members)
	}
}

// TestHub_PublishAsOperator_BypassesHalt is the Hub-level half of the say/halt
// asymmetry (docs/design/atomic-bus.md decision #4). Skipping the halt check is
// safe here only because the identity is pinned: halt binds agents, and a human
// is the one who lifts it.
func TestHub_PublishAsOperator_BypassesHalt(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := h.Halt("potato", "stop"); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	if _, err := h.Publish("potato", "sess-agent", nil, "", "still going"); err == nil {
		t.Fatal("expected agent Publish into a halted room to fail")
	}
	if _, err := h.PublishAsOperator("potato", nil, "", "hold on"); err != nil {
		t.Fatalf("expected operator publish to bypass halt, got: %v", err)
	}
}

func TestHub_PublishAsOperator_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.PublishAsOperator("nonexistent", nil, "", "hello")
	mustError(t, err, ExitNoRoom)
}

// TestHub_PublishAsOperator_IdentityIsAlwaysTheOperator is the regression test
// for the impersonation hole: whatever the caller does, every envelope this
// path produces carries the operator identity. There is no argument that can
// change it, which is the whole reason the parameters were removed.
func TestHub_PublishAsOperator_IdentityIsAlwaysTheOperator(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	for _, text := range []string{"one", "two", "three"} {
		env, err := h.PublishAsOperator("potato", []string{"backend"}, "", text)
		if err != nil {
			t.Fatalf("PublishAsOperator(%q): %v", text, err)
		}
		if env.From != operatorName {
			t.Errorf("From = %q, want %q", env.From, operatorName)
		}
		if env.FromKind != KindHuman {
			t.Errorf("FromKind = %q, want %q", env.FromKind, KindHuman)
		}
	}
}

// TestHub_PublishAsOperator_OversizedTextRejected proves this path shares
// Publish's validation limits via publishValidated rather than re-implementing
// — or forgetting — them.
func TestHub_PublishAsOperator_OversizedTextRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	oversized := strings.Repeat("a", MaxTextBytes+1)
	_, err := h.PublishAsOperator("potato", nil, "", oversized)
	mustError(t, err, ExitUsage)
}

// TestHub_Join_ReservedOperatorNameRejected closes the name-squatting half of
// the same hole: without it an agent could join as "human" and its sends would
// render identically to operator input in a tail transcript.
func TestHub_Join_ReservedOperatorNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Join("potato", operatorName, "normal", "agent", "sess-agent", "", "")
	mustError(t, err, ExitUsage)
}

// --- self-echo: fanOut skips a subscription's own session (finding 2 of
// docs/spec/atomic-bus.md's 2026-07-29 change-log entry) ---

// TestHub_Subscribe_SkipSelf_DoesNotReceiveOwnPublish is the regression test
// for the self-echo finding: before this fix, Hub.Subscribe carried no
// identity at all, so fanOut delivered every publish to every subscriber
// including its own sends.
func TestHub_Subscribe_SkipSelf_DoesNotReceiveOwnPublish(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 4)
	unsub := h.Subscribe("potato", ch, "sess-1", true)
	defer unsub()

	if _, err := h.Publish("potato", "sess-1", nil, "", "my own message"); err != nil {
		t.Fatalf("Publish (self): %v", err)
	}
	if _, err := h.Publish("potato", "sess-2", nil, "", "someone else's message"); err != nil {
		t.Fatalf("Publish (other): %v", err)
	}

	// The self-published message must never surface — the first (and only)
	// envelope this subscription sees is the other session's.
	env := recvEnvelope(t, ch)
	if env.Text != "someone else's message" {
		t.Fatalf("delivered = %q, want %q — the self-published message should have been skipped entirely, not merely reordered", env.Text, "someone else's message")
	}
}

// TestHub_Subscribe_SkipSelfFalse_StillReceivesOwnPublish is tail/chat's
// contract: a subscription that does not opt out must keep seeing its own
// session's publishes (docs/spec/atomic-bus.md: "tail and chat still see
// the complete transcript including their own lines").
func TestHub_Subscribe_SkipSelfFalse_StillReceivesOwnPublish(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "sess-1", false)
	defer unsub()

	if _, err := h.Publish("potato", "sess-1", nil, "", "my own message"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.Text != "my own message" {
		t.Fatalf("Text = %q, want the subscriber's own message delivered back to it", env.Text)
	}
}

// TestHub_Subscribe_SkipSelf_OperatorPublishAlwaysDelivered proves an empty
// publisherSession (PublishAsOperator's say/halt/resume path) can never
// match — and therefore never wrongly suppress — a skipSelf subscription,
// since a real session id assigned by Join is never empty.
func TestHub_Subscribe_SkipSelf_OperatorPublishAlwaysDelivered(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "sess-1", true)
	defer unsub()

	if _, err := h.PublishAsOperator("potato", nil, "", "operator speaking"); err != nil {
		t.Fatalf("PublishAsOperator: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.From != operatorName {
		t.Fatalf("From = %q, want %q — an operator publish must reach every subscriber regardless of skipSelf", env.From, operatorName)
	}
}

// --- resume: envelope body (finding 4) ---

// TestHub_Resume_EmptyText_PublishesDefaultBody is the regression test for
// the resume finding: resumeAction never had a --text flag, so every resume
// published Text == "" — a notification carrying nothing to act on.
func TestHub_Resume_EmptyText_PublishesDefaultBody(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 2)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	if err := h.Halt("potato", "stop"); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	recvEnvelope(t, ch) // the halt control envelope

	if err := h.Resume("potato", ""); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	env := recvEnvelope(t, ch)
	if env.Text == "" {
		t.Fatal("resume published an empty-body envelope")
	}
}

// TestHub_Resume_ExplicitText_Preserved proves a caller that does supply
// text is not overridden by the default.
func TestHub_Resume_ExplicitText_Preserved(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 2)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	if err := h.Halt("potato", "stop"); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	recvEnvelope(t, ch)

	if err := h.Resume("potato", "all clear, resuming"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	env := recvEnvelope(t, ch)
	if env.Text != "all clear, resuming" {
		t.Fatalf("Text = %q, want the explicit text preserved verbatim", env.Text)
	}
}

// TestHub_Halt_EmptyText_StaysEmpty pins the "do not change halt" half of
// the brief: an operator's empty --text on halt must not gain a synthetic
// default the way resume's did.
func TestHub_Halt_EmptyText_StaysEmpty(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "", false)
	defer unsub()

	if err := h.Halt("potato", ""); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	env := recvEnvelope(t, ch)
	if env.Text != "" {
		t.Fatalf("Text = %q, want empty — halt's empty --text must be left as-is, unlike resume's", env.Text)
	}
}

// --- last_seen, staleness, prune (finding 3) ---

// testClock is a controllable time source for staleness tests, injected via
// Hub.SetClock — advances deterministically, without a real sleep.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(start time.Time) *testClock {
	return &testClock{now: start}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestHub_Who_FreshMember_NotStale(t *testing.T) {
	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if members[0].Stale {
		t.Fatal("a freshly joined member must not be reported stale")
	}
}

// TestHub_Who_MemberStale_AfterThresholdWithNoActivityAndNoSubscription is
// the regression test for the "dead members were immortal" finding: nothing
// distinguished a session that exited from one still around, so `who` had
// no way to tell them apart — a roster from live testing showed five dead
// sessions, still listed, indistinguishable from live ones.
func TestHub_Who_MemberStale_AfterThresholdWithNoActivityAndNoSubscription(t *testing.T) {
	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	clock.Advance(staleThreshold + time.Second)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if !members[0].Stale {
		t.Fatal("expected the member to be reported stale after staleThreshold with no activity and no subscription")
	}
}

// TestHub_Who_LiveSubscription_NeverStale_RegardlessOfThreshold proves the
// override: a member holding an open recv/chat subscription is not stale no
// matter how long it has been since its last send — the subscription
// itself is ongoing proof of life (docs/spec/atomic-bus.md: "refreshed ...
// on an open subscription").
func TestHub_Who_LiveSubscription_NeverStale_RegardlessOfThreshold(t *testing.T) {
	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "sess-1", true)
	defer unsub()

	clock.Advance(staleThreshold * 10)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if members[0].Stale {
		t.Fatal("a member with a live subscription must never be reported stale")
	}
}

// TestHub_Who_Publish_RefreshesLastSeen proves the other half of "refreshed
// on any operation from that session": a send resets the staleness clock.
func TestHub_Who_Publish_RefreshesLastSeen(t *testing.T) {
	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)

	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	clock.Advance(staleThreshold - time.Minute)
	if _, err := h.Publish("potato", "sess-1", nil, "", "still here"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	clock.Advance(staleThreshold - time.Minute)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if members[0].Stale {
		t.Fatal("a send should have refreshed LastSeen, keeping the member fresh past the original threshold")
	}
}

// TestHub_Rehydrate_MemberNotImmediatelyStale proves a restarted daemon
// does not report a genuinely recent member as stale the instant it comes
// back up — Rehydrate restores the persisted LastSeen (here, freshly
// stamped by State.Join moments earlier) rather than discarding it
// (docs/spec/atomic-bus.md: "a member who has been idle across the restart
// is still ... addressable"). TestHub_Rehydrate_RestoresLastSeenNotRestamped
// below covers the complementary case — a member persisted as genuinely
// stale must read as stale immediately after rehydration, not fresh.
func TestHub_Rehydrate_MemberNotImmediatelyStale(t *testing.T) {
	home := t.TempDir()
	h1 := NewHub(home)
	if _, err := h1.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	st, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st.Join("sess-1", "potato", "backend", "normal", "agent", "", "")
	if err := st.Save(home); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stReloaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	h2 := NewHub(home)
	h2.Rehydrate(stReloaded)

	members, err := h2.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || members[0].Stale {
		t.Fatalf("members = %+v, want one fresh (non-stale) rehydrated member", members)
	}
}

// TestHub_Rehydrate_RestoresLastSeenNotRestamped is the regression test for
// the "last_seen must persist, not be restamped" fix: a member persisted as
// genuinely stale (LastSeen hours in the past) must read as stale
// immediately after Rehydrate, not fresh — the old behavior stamped
// LastSeen at rehydrate time unconditionally, resurrecting every idle
// member as freshly live and putting it permanently out of Prune's reach
// (docs/spec/atomic-bus.md's 2026-07-30 entry).
func TestHub_Rehydrate_RestoresLastSeenNotRestamped(t *testing.T) {
	longAgo := time.Now().Add(-3 * time.Hour)
	st := &State{Sessions: map[string]*sessionState{
		"sess-ghost": {Rooms: map[string]roomMembership{
			"potato": {Name: "ghost", Mode: "participate", Kind: KindAgent, Joined: longAgo, LastSeen: longAgo},
		}},
	}}

	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)
	h.Rehydrate(st)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || !members[0].Stale {
		t.Fatalf("members = %+v, want one member reported stale immediately (a ghost from hours ago is a usable identity if left non-stale)", members)
	}
}

// TestHub_Rehydrate_ZeroLastSeenFallsBackToJoined covers a bus.json written
// before LastSeen was persisted: Rehydrate must not leave the zero value in
// place (which would read as maximally stale regardless of how recently the
// member actually joined) — Joined is the best available signal for such an
// entry, and Hub.Join never leaves it zero.
func TestHub_Rehydrate_ZeroLastSeenFallsBackToJoined(t *testing.T) {
	recentJoin := time.Now().Add(-time.Minute)
	st := &State{Sessions: map[string]*sessionState{
		"sess-1": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: recentJoin}, // LastSeen left zero
		}},
	}}

	h := NewHub(t.TempDir())
	h.Rehydrate(st)

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members = %+v, want 1", members)
	}
	if members[0].Stale {
		t.Fatal("expected a recent Joined fallback to read as fresh, not stale")
	}
}

// TestHub_Rehydrate_RestoresHaltedFlagAndReason proves halt state comes
// back after a restart even for a room with no current members — a member
// row is not available to derive the halt flag from (docs/spec/atomic-bus.md's
// 2026-07-30 "halt must persist and be visible" entry).
func TestHub_Rehydrate_RestoresHaltedFlagAndReason(t *testing.T) {
	st := &State{Rooms: map[string]*roomState{
		"potato": {Halted: true, HaltText: "investigating a bad deploy"},
	}}

	h := NewHub(t.TempDir())
	h.Rehydrate(st)

	halted, reason, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if !halted {
		t.Fatal("expected potato to come back halted")
	}
	if reason != "investigating a bad deploy" {
		t.Fatalf("reason = %q, want %q", reason, "investigating a bad deploy")
	}
}

// TestHub_Rehydrate_UnhaltedRoomEntryDoesNothing proves a resumed (or
// never-halted) room's absence from st.Rooms — or a Halted:false entry, if
// one somehow existed — never spuriously halts a room on rehydrate.
func TestHub_Rehydrate_UnhaltedRoomEntryDoesNothing(t *testing.T) {
	st := &State{
		Sessions: map[string]*sessionState{
			"sess-1": {Rooms: map[string]roomMembership{
				"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: time.Now(), LastSeen: time.Now()},
			}},
		},
		Rooms: map[string]*roomState{"potato": {Halted: false}},
	}

	h := NewHub(t.TempDir())
	h.Rehydrate(st)

	halted, _, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if halted {
		t.Fatal("expected potato to not be halted")
	}
}

// TestHub_IsHalted_ReportsReason proves IsHalted's third return value
// actually carries the text Halt was given, not just the flag —
// handleWho/handleRooms depend on this to surface "why" alongside "halted".
func TestHub_IsHalted_ReportsReason(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := h.Halt("potato", "stop, wrong approach"); err != nil {
		t.Fatalf("Halt: %v", err)
	}

	halted, reason, err := h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if !halted || reason != "stop, wrong approach" {
		t.Fatalf("IsHalted = (%v, %q), want (true, %q)", halted, reason, "stop, wrong approach")
	}

	if err := h.Resume("potato", ""); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	halted, reason, err = h.IsHalted("potato")
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if halted || reason != "" {
		t.Fatalf("IsHalted after Resume = (%v, %q), want (false, \"\") — the reason must clear too", halted, reason)
	}
}

// --- close ---

// TestHub_Close_PublishesEnvelopeEvictsMembersAndDropsRoom proves the three
// observable effects of Close in one place: a "room closed" control
// envelope lands in the room log, the roster is empty afterward (the room
// itself is gone), and a subsequent operation against the same room name
// sees ExitNoRoom rather than a leftover empty room.
func TestHub_Close_PublishesEnvelopeEvictsMembersAndDropsRoom(t *testing.T) {
	home := t.TempDir()
	h := NewHub(home)
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Join("potato", "frontend", "normal", "agent", "sess-2", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	if err := h.Close("potato"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := h.Who("potato"); err == nil {
		t.Fatal("expected the room to no longer exist after Close")
	} else {
		mustError(t, err, ExitNoRoom)
	}

	entries := readRoomLog(t, home, "potato")
	if len(entries) != 1 {
		t.Fatalf("room log has %d entries, want 1", len(entries))
	}
	if entries[0].Text != "room closed" || entries[0].From != systemName {
		t.Fatalf("closing envelope = %+v, want text %q from %q", entries[0], "room closed", systemName)
	}
	if !entries[0].Closing {
		t.Fatal("closing envelope must carry Closing:true — recvDeliver's reconnect-vs-stop decision depends on it")
	}
}

// TestHub_Close_UnknownRoomReturnsExitNoRoom mirrors Halt's own contract for
// a room that was never joined or already dropped.
func TestHub_Close_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	err := h.Close("nonexistent")
	mustError(t, err, ExitNoRoom)
}

// TestHub_Close_TerminatesLiveSubscribersStream proves "subscribers' streams
// end after they receive that envelope": a live subscriber's channel
// receives the closing envelope and is then closed, not merely left
// dangling on a room that no longer exists.
func TestHub_Close_TerminatesLiveSubscribersStream(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	ch := make(chan Envelope, 1)
	unsub := h.Subscribe("potato", ch, "sess-1", false)
	defer unsub()

	if err := h.Close("potato"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	env := recvEnvelope(t, ch)
	if env.Text != "room closed" {
		t.Fatalf("subscriber's final envelope text = %q, want %q", env.Text, "room closed")
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the subscriber's channel to be closed after the closing envelope, got another value")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber's channel was never closed after Close")
	}
}

// TestHub_Close_DoesNotDeleteTheRoomLog proves the room log on disk survives
// Close — it is the durable record, and a roster operation must not delete
// it (docs/spec/atomic-bus.md: "the room log stays on disk").
func TestHub_Close_DoesNotDeleteTheRoomLog(t *testing.T) {
	home := t.TempDir()
	h := NewHub(home)
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	if _, err := h.Publish("potato", "sess-1", nil, "", "hello before close"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if err := h.Close("potato"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(RoomLogPath(home, "potato")); err != nil {
		t.Fatalf("expected room log to still exist on disk after Close: %v", err)
	}
	entries := readRoomLog(t, home, "potato")
	if len(entries) != 2 {
		t.Fatalf("room log has %d entries, want 2 (the pre-close message plus the closing envelope)", len(entries))
	}
}

// TestHub_Prune_RemovesOnlyStaleMembers is the regression test for the
// missing prune verb: a stale member must be removed, a fresh one left
// alone, in the same room.
func TestHub_Prune_RemovesOnlyStaleMembers(t *testing.T) {
	h := NewHub(t.TempDir())
	clock := newTestClock(time.Now())
	h.SetClock(clock.Now)

	if _, err := h.Join("potato", "ghost", "normal", "agent", "sess-ghost", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	clock.Advance(staleThreshold + time.Second)
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-fresh", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	removed, err := h.Prune("potato")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != "ghost" {
		t.Fatalf("removed = %v, want [ghost]", removed)
	}

	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 || members[0].Name != "backend" {
		t.Fatalf("members after prune = %+v, want only backend to remain", members)
	}
}

// TestHub_Prune_NoStaleMembers_RemovesNothing proves prune never touches a
// live roster — nothing here is auto-reaped, only what isStale already
// flags (docs/spec/atomic-bus.md: "nothing reaps a member silently").
func TestHub_Prune_NoStaleMembers_RemovesNothing(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-1", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}

	removed, err := h.Prune("potato")
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}
	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members = %+v, want the one still-live member untouched", members)
	}
}

func TestHub_Prune_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Prune("nonexistent")
	mustError(t, err, ExitNoRoom)
}
