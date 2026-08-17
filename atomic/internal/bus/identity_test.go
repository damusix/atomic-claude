package bus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionID_ReadsEnvVar(t *testing.T) {
	t.Setenv(sessionEnvVar, "abc-123")

	id, err := SessionID("")
	if err != nil {
		t.Fatalf("SessionID: %v", err)
	}
	if id != "abc-123" {
		t.Fatalf("SessionID = %q, want %q", id, "abc-123")
	}
}

func TestSessionID_OverrideWinsOverEnv(t *testing.T) {
	t.Setenv(sessionEnvVar, "from-env")

	id, err := SessionID("from-flag")
	if err != nil {
		t.Fatalf("SessionID: %v", err)
	}
	if id != "from-flag" {
		t.Fatalf("SessionID = %q, want override %q", id, "from-flag")
	}
}

// Identity is keyed by session id with no cwd or pid fallback. Two Claude Code
// sessions in one directory are two distinct agents; deriving an identity from
// anything else would collapse that distinction.
func TestSessionID_AbsentIsAClearErrorNeverAFallback(t *testing.T) {
	t.Setenv(sessionEnvVar, "") // os.Getenv treats set-to-empty same as unset

	_, err := SessionID("")
	if err == nil {
		t.Fatal("expected an error when CLAUDE_CODE_SESSION_ID is unset and no override is given")
	}

	busErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("expected *bus.Error, got %T: %v", err, err)
	}
	if busErr.Code != ExitHard {
		t.Fatalf("Code = %d, want ExitHard (%d)", busErr.Code, ExitHard)
	}
	if !strings.Contains(busErr.Msg, sessionEnvVar) {
		t.Fatalf("error message should name %s so the user knows what to set; got: %s", sessionEnvVar, busErr.Msg)
	}
}

func TestState_LoadMissingFileYieldsEmptyState(t *testing.T) {
	home := t.TempDir()

	st, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Sessions) != 0 {
		t.Fatalf("expected no sessions in a freshly created state, got %d", len(st.Sessions))
	}
}

// Points HOME at a temp dir, calls the production Save/Load, and asserts against
// the literal path production reads — computed independently of StatePath, so a
// bug in StatePath itself cannot hide behind this test.
func TestState_SaveLoadRoundTrip_RealFileOnDisk(t *testing.T) {
	// home is passed explicitly; nothing in this package reads $HOME. Resolving
	// it is the CLI dispatch layer's job.
	home := t.TempDir()

	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")

	if err := st.Save(home); err != nil {
		t.Fatalf("Save: %v", err)
	}

	wantPath := filepath.Join(home, ".atomic", "bus.json")
	raw, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("expected state file at %s: %v", wantPath, err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("file on disk is not valid JSON: %v", err)
	}

	reloaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	room, ok := reloaded.LastRoom("sess-1")
	if !ok || room != "potato" {
		t.Fatalf("LastRoom after reload = (%q, %v), want (%q, true)", room, ok, "potato")
	}
}

// repo/realm survive the Save/Load round trip a fresh `atomic bus` invocation
// depends on. TestHub_Rehydrate_RestoresRepoAndRealm covers the daemon side.
func TestState_Join_PersistsRepoAndRealmAcrossSaveLoad(t *testing.T) {
	home := t.TempDir()

	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "backend", "participate", KindAgent, "atomic-claude", "myrealm")
	if err := st.Save(home); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := reloaded.Sessions["sess-1"].Rooms["potato"]
	if m.Repo != "atomic-claude" {
		t.Errorf("Repo = %q, want %q", m.Repo, "atomic-claude")
	}
	if m.Realm != "myrealm" {
		t.Errorf("Realm = %q, want %q", m.Realm, "myrealm")
	}
}

func TestState_LastRoom_ReturnsMostRecentJoin(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")
	st.Join("sess-1", "carrot", "frontend", "participate", KindAgent, "", "")

	room, ok := st.LastRoom("sess-1")
	if !ok {
		t.Fatal("expected a last room")
	}
	if room != "carrot" {
		t.Fatalf("LastRoom = %q, want %q (the most recently joined)", room, "carrot")
	}
}

func TestState_LastRoom_UnjoinedSessionReturnsFalse(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}

	if _, ok := st.LastRoom("nobody"); ok {
		t.Fatal("expected ok=false for a session that has never joined anything")
	}
}

// Leave does not merely clear LastRoom when the departed room was the most
// recent — it falls back to whatever remains joined, so a --room-less command
// still has a sensible default.
func TestState_Leave_RecomputesLastRoomFromRemaining(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")
	st.Join("sess-1", "carrot", "frontend", "participate", KindAgent, "", "") // becomes LastRoom

	st.Leave("sess-1", "carrot")

	room, ok := st.LastRoom("sess-1")
	if !ok {
		t.Fatal("expected potato to remain as the last room after leaving carrot")
	}
	if room != "potato" {
		t.Fatalf("LastRoom after Leave = %q, want %q", room, "potato")
	}
}

func TestState_Leave_LastRoomUnsetWhenNoRoomsRemain(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")

	st.Leave("sess-1", "potato")

	if _, ok := st.LastRoom("sess-1"); ok {
		t.Fatal("expected no last room once every joined room has been left")
	}
}

func TestState_ResolveRoom(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")

	t.Run("explicit room wins over last joined", func(t *testing.T) {
		room, err := st.ResolveRoom("sess-1", "carrot")
		if err != nil {
			t.Fatalf("ResolveRoom: %v", err)
		}
		if room != "carrot" {
			t.Fatalf("room = %q, want %q", room, "carrot")
		}
	})

	t.Run("falls back to last joined room", func(t *testing.T) {
		room, err := st.ResolveRoom("sess-1", "")
		if err != nil {
			t.Fatalf("ResolveRoom: %v", err)
		}
		if room != "potato" {
			t.Fatalf("room = %q, want %q", room, "potato")
		}
	})

	// ResolveRoom must return a not-joined error when the session has joined
	// nothing.
	t.Run("not-joined session errors with ExitNotJoined", func(t *testing.T) {
		_, err := st.ResolveRoom("sess-never-joined", "")
		if err == nil {
			t.Fatal("expected a not-joined error")
		}
		busErr, ok := err.(*Error)
		if !ok {
			t.Fatalf("expected *bus.Error, got %T: %v", err, err)
		}
		if busErr.Code != ExitNotJoined {
			t.Fatalf("Code = %d, want ExitNotJoined (%d)", busErr.Code, ExitNotJoined)
		}
	})
}

func TestEnsureDirs_CreatesRoomsDirAtRestrictivePerms(t *testing.T) {
	home := t.TempDir()

	if err := EnsureDirs(home); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	info, err := os.Stat(filepath.Join(home, ".atomic", "rooms"))
	if err != nil {
		t.Fatalf("expected rooms dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("rooms path exists but is not a directory")
	}
	// Room logs are the durable record and Unix file permissions are bus's only
	// authentication, so 0700 keeps them private to this user.
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("rooms dir perm = %o, want 0700", perm)
	}
}

// Hub.Rehydrate treats a zero LastSeen as "written before this field existed",
// so Join must never leave it zero.
func TestState_Join_StampsLastSeenEqualToJoined(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")

	m := st.Sessions["sess-1"].Rooms["potato"]
	if m.LastSeen.IsZero() {
		t.Fatal("LastSeen is zero right after Join")
	}
	if !m.LastSeen.Equal(m.Joined) {
		t.Fatalf("LastSeen = %v, want equal to Joined %v", m.LastSeen, m.Joined)
	}
}

// The persisted counterpart to Hub.Publish's in-memory refresh: sendAction calls
// it after a successful send so a later Rehydrate has an honest timestamp to
// restore rather than "now".
func TestState_TouchLastSeen_UpdatesExistingMembership(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")
	joinedAt := st.Sessions["sess-1"].Rooms["potato"].LastSeen

	later := joinedAt.Add(time.Hour)
	if ok := st.TouchLastSeen("sess-1", "potato", later); !ok {
		t.Fatal("TouchLastSeen returned false for an existing membership")
	}

	got := st.Sessions["sess-1"].Rooms["potato"].LastSeen
	if !got.Equal(later) {
		t.Fatalf("LastSeen = %v, want %v", got, later)
	}
}

// The send-then-leave race across processes: nothing to touch, and the caller
// must be able to tell rather than silently writing a fresh entry.
func TestState_TouchLastSeen_UnknownMembershipReturnsFalse(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	if ok := st.TouchLastSeen("sess-never-joined", "potato", time.Now()); ok {
		t.Fatal("expected false for a session with no membership in the room")
	}

	st.Join("sess-1", "carrot", "frontend", "participate", KindAgent, "", "")
	if ok := st.TouchLastSeen("sess-1", "potato", time.Now()); ok {
		t.Fatal("expected false for a room this session never joined")
	}
}

// The disk half — the TouchLastSeen tests above only prove the in-memory
// mutation, while a restarted daemon reads this back through Load.
func TestState_LastSeen_SurvivesSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "backend", "participate", KindAgent, "", "")
	staleTime := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	st.TouchLastSeen("sess-1", "potato", staleTime)

	if err := st.Save(home); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := reloaded.Sessions["sess-1"].Rooms["potato"].LastSeen
	if !got.Equal(staleTime) {
		t.Fatalf("LastSeen after reload = %v, want %v (not restamped to now)", got, staleTime)
	}
}

// Recording a halt with its reason, and a resume removing the entry outright:
// absent and Halted:false mean the same thing.
func TestState_SetHalted_PersistsAndClears(t *testing.T) {
	st := &State{}
	st.SetHalted("potato", true, "investigating a bad deploy")

	rs, ok := st.Rooms["potato"]
	if !ok || !rs.Halted || rs.HaltText != "investigating a bad deploy" {
		t.Fatalf("Rooms[potato] = %+v, ok=%v, want Halted=true with the given text", rs, ok)
	}

	st.SetHalted("potato", false, "")
	if _, ok := st.Rooms["potato"]; ok {
		t.Fatal("expected the room entry to be removed on resume, not left as Halted:false")
	}
}

// The disk half: an operator who halts a room and walks away needs it still
// halted after a `bus restart`, not merely in the process that called SetHalted.
func TestState_Halted_SurvivesSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	st := &State{}
	st.SetHalted("potato", true, "stop, wrong approach")
	if err := st.Save(home); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rs, ok := reloaded.Rooms["potato"]
	if !ok || !rs.Halted || rs.HaltText != "stop, wrong approach" {
		t.Fatalf("reloaded Rooms[potato] = %+v, ok=%v, want the halt to survive intact", rs, ok)
	}
}

// The bus.json-side half of Hub.Close: closing a room clears other sessions'
// entries too, unlike Leave, which is scoped to the calling session.
func TestState_ClearRoom_RemovesEveryonesMembershipAndHaltState(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-fe", "potato", "frontend", "participate", KindAgent, "", "")
	st.Join("sess-be", "potato", "backend", "participate", KindAgent, "", "")
	st.Join("sess-fe", "carrot", "frontend", "participate", KindAgent, "", "") // becomes sess-fe's LastRoom
	st.SetHalted("potato", true, "operator halted this room")

	st.ClearRoom("potato")

	if _, ok := st.Sessions["sess-fe"].Rooms["potato"]; ok {
		t.Error("sess-fe's potato membership survived ClearRoom")
	}
	if _, ok := st.Sessions["sess-be"].Rooms["potato"]; ok {
		t.Error("sess-be's potato membership survived ClearRoom")
	}
	if _, ok := st.Rooms["potato"]; ok {
		t.Error("potato's persisted halt state survived ClearRoom")
	}
	// carrot is untouched — ClearRoom only targets the named room.
	if _, ok := st.Sessions["sess-fe"].Rooms["carrot"]; !ok {
		t.Error("ClearRoom(\"potato\") should not have touched sess-fe's carrot membership")
	}
}

// Mirrors Leave's LastRoom recompute: a session whose most recent room is closed
// by someone else still needs a sensible --room-less default.
func TestState_ClearRoom_RecomputesLastRoomWhenCleared(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend", "participate", KindAgent, "", "")
	st.Join("sess-1", "carrot", "frontend", "participate", KindAgent, "", "") // becomes LastRoom

	st.ClearRoom("carrot")

	room, ok := st.LastRoom("sess-1")
	if !ok || room != "potato" {
		t.Fatalf("LastRoom after ClearRoom(carrot) = (%q, %v), want (%q, true)", room, ok, "potato")
	}
}

func TestPathHelpers_ResolveUnderAtomicHome(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".atomic")

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"SocketPath", SocketPath(home), filepath.Join(root, "bus.sock")},
		{"LockPath", LockPath(home), filepath.Join(root, "bus.lock")},
		{"StatePath", StatePath(home), filepath.Join(root, "bus.json")},
		{"RoomLogPath", RoomLogPath(home, "potato"), filepath.Join(root, "rooms", "potato.log")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
