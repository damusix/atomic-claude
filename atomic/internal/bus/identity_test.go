package bus

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestSessionID_AbsentIsAClearErrorNeverAFallback locks in hard constraint 2
// of the atomic-bus brief: identity is keyed by session id, with no cwd or
// pid fallback. Two Claude Code sessions in the same working directory are
// two distinct agents; silently deriving an identity from anything else
// would collapse that distinction.
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

// TestState_SaveLoadRoundTrip_RealFileOnDisk is the mandatory end-to-end
// test called out in .claude/skills/atomic-cli-contrib/SKILL.md §3: it
// points HOME at a temp dir, calls the production entry points
// (State.Save / Load), and asserts against the literal path production code
// would read — computed independently of StatePath, not through it, so a
// bug in StatePath itself could not hide behind this test.
func TestState_SaveLoadRoundTrip_RealFileOnDisk(t *testing.T) {
	// home is passed explicitly to Save/Load; nothing in this package reads
	// $HOME, so there is no env var to point here. Resolving $HOME into
	// `home` is the CLI dispatch layer's job, covered once that layer exists.
	home := t.TempDir()

	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend")

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

func TestState_LastRoom_ReturnsMostRecentJoin(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend")
	st.Join("sess-1", "carrot", "frontend")

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

// TestState_Leave_RecomputesLastRoomFromRemaining proves Leave does not
// merely clear LastRoom when the departed room was the most recent one — it
// falls back to whatever room remains joined, so a --room-less command
// still has a sensible default after leaving one of several rooms.
func TestState_Leave_RecomputesLastRoomFromRemaining(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend")
	st.Join("sess-1", "carrot", "frontend") // becomes LastRoom

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
	st.Join("sess-1", "potato", "frontend")

	st.Leave("sess-1", "potato")

	if _, ok := st.LastRoom("sess-1"); ok {
		t.Fatal("expected no last room once every joined room has been left")
	}
}

func TestState_ResolveRoom(t *testing.T) {
	st := &State{Sessions: map[string]*sessionState{}}
	st.Join("sess-1", "potato", "frontend")

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

	// docs/spec/atomic-bus.md success criteria: "ResolveRoom ... a
	// not-joined error (exit code 3) when the session has joined nothing."
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
	// Room logs are the durable record of every room's traffic, and bus's
	// only authentication is Unix file permissions — 0700 keeps them
	// private to this user.
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("rooms dir perm = %o, want 0700", perm)
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
