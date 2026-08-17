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

// subscribeTimeout bounds every subscriber-channel assertion here: a missed
// publish must fail with a clear message, never hang the suite.
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

// Join stores whatever repo/realm the caller reports directly on the roster —
// Member is the record of a resolved position, not merely a name.
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

// An empty realm at Join stays empty on the roster, never a placeholder.
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

// A session that joins the same room twice must not leak a stale roster entry
// under its old name — otherwise a retried join accumulates ghost members Who
// reports as present.
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

// Join once deleted a session's prior roster entry before confirming the new
// name was claimable, so an ExitNameTaken failure left bySession pointing at a
// name no longer in members: Who undercounted, and the next Publish carried a
// stale From with an empty FromKind. A failed Join must be a roster no-op.
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

	// sess-1 rejoins as "worker", taken in both its bare and "-2" forms.
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

// The name claim must be atomic, not merely unlikely to collide. N goroutines
// race to join under one name; the outcome distribution is exactly {1 exact-name
// winner, 1 "-2" winner, N-2 ExitNameTaken failures}, never scheduling-
// dependent. Run with -race: the point of Hub.mu is that this cannot race.
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

	// The roster must agree with the outcome: exactly two members. A lost update
	// shows up as a missing or duplicated entry even when the returned names
	// looked right above.
	members, err := h.Who("potato")
	if err != nil {
		t.Fatalf("Who: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("roster has %d members after the race, want exactly 2: %+v", len(members), members)
	}
}

// A client could once Join as "system", making every Publish from it
// bit-for-bit identical to a daemon control envelope and defeating the
// guarantee those exist for. The rejected Join must also be a no-op.
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

// kind was once unvalidated and stamped verbatim onto every envelope's
// from_kind — including "system", which let a spoofed member match fanOut's
// drop marker. It must be one of the two documented values or Join fails.
func TestHub_Join_InvalidKindRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Join("potato", "backend", "normal", "hologram", "sess-1", "", "")
	mustError(t, err, ExitUsage)

	if rooms := h.Rooms(); len(rooms) != 0 {
		t.Fatalf("expected no room created by a rejected Join, got %v", rooms)
	}
}

// An unbounded room name lands in every envelope's Room field and grows the room
// log without bound — the failure class MaxTextBytes closed for Text.
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

// The same cap for a member's assigned name, which lands in every From field.
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
// A restarted daemon rehydrates the whole roster at once, not one session at a
// time, and mode and kind survive the restart.

func TestHub_Rehydrate_RestoresWholeRosterAcrossMultipleRoomsAndSessions(t *testing.T) {
	h := NewHub(t.TempDir())
	st := &State{Sessions: map[string]*sessionState{
		"sess-fe": {Rooms: map[string]roomMembership{
			"potato": {Name: "frontend", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
		"sess-be": {Rooms: map[string]roomMembership{
			"potato": {Name: "backend", Mode: "participate", Kind: KindAgent, Joined: time.Now()},
		}},
		// judge never runs another command after the restart — rehydration must
		// not depend on that, unlike the per-session re-registration it replaced.
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

// A bus.json written before Mode/Kind existed: Rehydrate must default exactly as
// a fresh Join would, not leave the zero value on the roster.
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

// repo/realm survive a restart via rehydration, exactly like mode/kind.
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

// Rehydrate bypasses Join's collision retry: two sessions that collided on
// "backend" (the second became "backend-2") come back under those exact names.
// A fresh Join replaying the claims in map order could not know "backend-2" was
// legitimately held by someone else.
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

// --- --to resolution: exact match, then unique suffix/substring ---

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

// The suffix case, separate from the substring case above: a suffix is a
// substring that ends the string, so one strings.Contains scan covers both.
// Narrowing to strings.HasSuffix would still pass the substring test above but
// would stop resolving a pure mid-string match, which this pins.
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

// Ambiguity is an error naming every candidate, never a silent pick, and its
// text is distinguishable from UnknownAddressees's "not currently in room"
// warning — the two mean different things.
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

// An ambiguous --to aborts the whole send rather than delivering under a
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
	// A second member keeps the room non-empty, so this proves member removal
	// independent of the auto-drop-when-empty behavior covered below.
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

// A room created by a typo, or simply finished with, must not linger forever
// with zero members.
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

// The guard on that auto-drop: a tail or recv holding an open subscription on an
// otherwise-empty room must not be orphaned. Dropping the room means any future
// Publish builds a new Room with an empty subs map, and this subscriber never
// hears anything again, with no error to explain why.
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
	// A memberless room has no member to publish as, so PublishAsOperator is the
	// right path here — the same way `say` reaches a room with no roster entry.
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

// rooms must report a per-room member count, not merely names. potato and carrot
// get different counts specifically to catch a fix that reports a count but
// always the same wrong one.
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

// dropIfEmpty drops only a room with neither members nor subscribers, so a room
// a tail is still watching keeps appearing with an honest Members == 0.
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

// The id must be a short opaque string, not the sequential base36 counter this
// used to be. A counter's first id is always "1" regardless of format, so
// asserting the id is neither "1" nor purely numeric catches a regression back
// to a counter even if someone reformats its base.
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

// A per-process counter reset to zero on every restart, so a fresh Hub's first
// message always got id "1" — indistinguishable from the previous daemon's
// first message in the same durable log. Two Hub instances publishing into one
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

	// A fresh Hub against the same home, exactly what a respawned daemon builds:
	// its roster and id bookkeeping start over from nothing.
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

// nextEnvelopeID's usedIDs guard must hold under real volume, not just a couple
// of calls.
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

// The room log is written unconditionally, not only when someone is watching.
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

// A subscriber that never drains its channel must not stall Publish. Bounded by
// a timeout so a regression to a blocking send hangs this test, not the suite.
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
		// Well past subscriberBuffer, so a blocking-send implementation would
		// overflow many times over.
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

// A message over MaxTextBytes is rejected before it reaches the room log — a
// bound on how large a single log line can grow.
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

// The same failure class as the oversized-Text case, for a field MaxTextBytes
// never covered.
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

// The addressee count is capped independently of their combined length — see
// MaxAddressees for why both caps are needed.
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

// The cap is on combined raw length, not just count: two entries well under
// MaxAddressees whose lengths sum past MaxAddresseesBytes are still rejected.
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

// A room with existing traffic must not replay any of it to a new subscriber.
// The prior ring-backed Since("") returned the entire ring, so a recv on a busy
// room delivered old messages as Monitor notifications, each acted on as if
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

// tail — and any other pure Subscribe caller — never appears in Who and never
// claims a name.
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

// A dropped envelope must never be indistinguishable from "nothing was sent":
// once a subscriber's buffer overflows, the next envelope that fits is preceded
// by a marker naming the drop count and the room log path.
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

// The asymmetry the whole halt feature exists for: an agent's send is rejected
// and a human's is not, which is what lets an operator still speak into — and
// thereby direct — a room they have stopped.
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

// resume actually flips the enforced flag back; halt does not merely wear off.
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

// setHalted once flipped r.halted before the durable append, so on an Append
// failure the operator's error implied the halt might not have taken while the
// room was in fact halted with nothing logged to prove it. The failure is forced
// deterministically by making the log's parent directory unwritable.
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

// halt binds by being observable, not only by rejecting the next send: a
// watching subscriber sees the announcement as a normal envelope.
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
// Append is exercised directly here rather than only through Hub.Publish. Reads
// go at the on-disk JSONL file because roomlog.go exposes no read path: the room
// log is the durable record, not something the daemon reads back.

// readRoomLog reads every envelope in room's log, in append order. A missing file
// yields an empty, nil-error result, mirroring Append's own never-had-a-message
// case.
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

// A message right at the limit Publish admits must always read back intact.
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

// Every capped field simultaneously at its admitted limit — Room, From, ReplyTo
// at MaxIdentifierBytes, To at MaxAddressees entries summing to exactly
// MaxAddresseesBytes, Text at MaxTextBytes — must still round-trip, not merely
// Text alone.
//
// The fill byte is 0x01, not a letter: a plain ASCII byte marshals to itself, so
// a same-length fill would only prove maximum length round-trips. 0x01 has no
// short JSON escape, so it marshals to the full 6-byte \u0001 — the worst-case
// escaped size a Publish-admitted envelope can reach on disk.
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

// Append's own file-level concurrency safety, proven independently of Hub.mu,
// which already serializes every real Publish.
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
// The sender identity is not a parameter. An earlier signature took name and
// kind from the caller and was reachable from the wire via OpSay, which let a raw
// request claim an agent's name and publish into a halted room. These tests pin
// the properties that fix relies on.

// The defining property: an envelope lands and the roster is untouched — say
// never occupies a name, mirroring tail's own no-roster-footprint guarantee.
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

// The Hub-level half of the say/halt asymmetry. Skipping the halt check is safe
// only because the identity is pinned: halt binds agents, a human lifts it.
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

// The impersonation hole: whatever the caller does, every envelope this path
// produces carries the operator identity. No argument can change it, which is
// why the parameters were removed.
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

// This path shares Publish's validation via publishValidated rather than
// re-implementing — or forgetting — it.
func TestHub_PublishAsOperator_OversizedTextRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	if _, err := h.Join("potato", "backend", "normal", "agent", "sess-agent", "", ""); err != nil {
		t.Fatalf("Join: %v", err)
	}
	oversized := strings.Repeat("a", MaxTextBytes+1)
	_, err := h.PublishAsOperator("potato", nil, "", oversized)
	mustError(t, err, ExitUsage)
}

// The name-squatting half of the same hole: without it an agent could join as
// "human" and render identically to operator input in a tail transcript.
func TestHub_Join_ReservedOperatorNameRejected(t *testing.T) {
	h := NewHub(t.TempDir())
	_, err := h.Join("potato", operatorName, "normal", "agent", "sess-agent", "", "")
	mustError(t, err, ExitUsage)
}

// --- self-echo: fanOut skips a subscription's own session ---

// Hub.Subscribe once carried no identity at all, so fanOut delivered every
// publish to every subscriber, including its own sends.
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

	// The self-published message must never surface — the only envelope this
	// subscription sees is the other session's.
	env := recvEnvelope(t, ch)
	if env.Text != "someone else's message" {
		t.Fatalf("delivered = %q, want %q — the self-published message should have been skipped entirely, not merely reordered", env.Text, "someone else's message")
	}
}

// tail/chat's contract: a subscription that does not opt out keeps seeing its
// own session's publishes.
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

// An empty publisherSession (the say/halt/resume path) can never match, and so
// never wrongly suppress, a skipSelf subscription: a session id from Join is
// never empty.
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

// --- resume: envelope body ---

// resumeAction never had a --text flag, so every resume published Text == "" —
// a notification carrying nothing to act on.
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

// A caller that does supply text is not overridden by the default.
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

// The do-not-change-halt half: an empty --text on halt must not gain a synthetic
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

// --- last_seen, staleness, prune ---

// testClock is a controllable time source injected via Hub.SetClock, advancing
// deterministically without a real sleep.
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

// Dead members were once immortal: nothing distinguished a session that exited
// from one still around, so a roster from live testing showed five dead sessions
// indistinguishable from live ones.
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

// A member holding an open subscription is not stale however long since its last
// send — the subscription is ongoing proof of life.
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

// A send resets the staleness clock.
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

// A restarted daemon must not report a genuinely recent member as stale the
// instant it comes back: Rehydrate restores the persisted LastSeen rather than
// discarding it. TestHub_Rehydrate_RestoresLastSeenNotRestamped covers the
// complementary case.
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

// A member persisted as genuinely stale must read as stale immediately after
// Rehydrate. The old behavior stamped LastSeen at rehydrate time
// unconditionally, resurrecting every idle member as live and putting it
// permanently out of Prune's reach.
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

// A bus.json written before LastSeen was persisted: the zero value would read as
// maximally stale however recently the member joined, so Rehydrate falls back to
// Joined, which Hub.Join never leaves zero.
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

// Halt state comes back after a restart even for a room with no members — there
// is no member row to derive the flag from.
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

// A resumed or never-halted room — absent from st.Rooms, or a Halted:false entry
// if one somehow existed — must never spuriously halt on rehydrate.
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

// IsHalted's third return carries the text Halt was given, not just the flag;
// handleWho/handleRooms depend on it to surface "why" alongside "halted".
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

// Close's three observable effects in one place: a "room closed" envelope lands
// in the log, the roster is empty afterward, and a later operation on the same
// room name sees ExitNoRoom rather than a leftover empty room.
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

// Mirrors Halt's contract for a room never joined or already dropped.
func TestHub_Close_UnknownRoomReturnsExitNoRoom(t *testing.T) {
	h := NewHub(t.TempDir())
	err := h.Close("nonexistent")
	mustError(t, err, ExitNoRoom)
}

// A live subscriber's channel receives the closing envelope and is then closed,
// not left dangling on a room that no longer exists.
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

// The room log survives Close: it is the durable record, and a roster operation
// must not delete it.
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

// A stale member is removed and a fresh one left alone, in the same room.
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

// prune never touches a live roster — only what isStale already flags.
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
