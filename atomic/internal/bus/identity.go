package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sessionEnvVar is the environment variable a live Claude Code session sets
// to a UUID identifying that session (verified present in a live session;
// see docs/design/atomic-bus.md, "Identity").
const sessionEnvVar = "CLAUDE_CODE_SESSION_ID"

// SessionID identifies the current agent for bus purposes. Identity is
// keyed by session id, never cwd or pid: two Claude Code sessions can run
// in the same working directory and are still two distinct agents (hard
// constraint 2 of the atomic-bus brief). override, when non-empty, is the
// --session flag documented in docs/design/atomic-bus.md's "Resolved open
// decisions" #5 — for scripted use and tests outside a live session.
func SessionID(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	id := os.Getenv(sessionEnvVar)
	if id == "" {
		return "", &Error{
			Code: ExitHard,
			Msg:  fmt.Sprintf("bus: %s is not set (not running inside a live Claude Code session); pass --session to identify this agent", sessionEnvVar),
		}
	}
	return id, nil
}

// roomMembership is one session's record of a joined room: the name it
// ended up with there (a join may be renamed by the daemon's numeric-suffix
// retry — see docs/design/atomic-bus.md's join flow, step 6), its kind and
// mode, and when. Mode and Kind exist so a restarted daemon can rehydrate a
// member exactly as it joined (room.go's Hub.Rehydrate) — before this they
// were held only in the daemon's memory, so any daemon restart silently
// reset every observer back to participate (docs/spec/atomic-bus.md:
// "mode and kind survive a daemon restart").
type roomMembership struct {
	Name   string    `json:"name"`
	Mode   string    `json:"mode,omitempty"`
	Kind   string    `json:"kind,omitempty"`
	Joined time.Time `json:"joined"`

	// Repo and Realm mirror Member.Repo/Member.Realm — persisted so a
	// restarted daemon's Hub.Rehydrate restores position exactly as
	// Hub.Join originally recorded it, the same reason Mode and Kind are
	// here (docs/spec/atomic-bus.md: "mode, kind, repo, and realm all
	// survive a daemon restart via bus.json rehydration").
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`
}

// sessionState is one session's bus.json entry.
type sessionState struct {
	Rooms map[string]roomMembership `json:"rooms"`
	// LastRoom is the most recently joined room, used by ResolveRoom to
	// default a --room-less command. It is recomputed on Leave rather
	// than merely cleared, so leaving the most recent room still leaves
	// a sensible default behind if other rooms remain joined.
	LastRoom string `json:"last_room,omitempty"`
}

// State is the per-session joined-room map persisted at ~/.atomic/bus.json.
// Every atomic bus CLI invocation is a fresh process; this file — not
// process memory, not the daemon — is the only thing that remembers what a
// session has joined between invocations.
type State struct {
	Sessions map[string]*sessionState `json:"sessions"`
}

// Load reads State from <home>/.atomic/bus.json. A missing file is not an
// error — it means no session has ever joined a room — and yields an empty
// State ready for use.
func Load(home string) (*State, error) {
	path := StatePath(home)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Sessions: map[string]*sessionState{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("bus: read state %s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("bus: parse state %s: %w", path, err)
	}
	if st.Sessions == nil {
		st.Sessions = map[string]*sessionState{}
	}
	return &st, nil
}

// Save writes State to <home>/.atomic/bus.json, creating the parent
// directory if needed. Uses write-to-tmp + rename for interrupt safety,
// matching config.WritePersist's pattern.
func (s *State) Save(home string) error {
	path := StatePath(home)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("bus: mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("bus: marshal state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".bus-*.json.tmp")
	if err != nil {
		return fmt.Errorf("bus: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("bus: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("bus: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("bus: rename to %s: %w", path, err)
	}
	return nil
}

// Join records that session has joined room under name with the given mode,
// kind, and resolved position (repo/realm), and marks room as the session's
// most recent join.
func (s *State) Join(session, room, name, mode, kind, repo, realm string) {
	if s.Sessions == nil {
		s.Sessions = map[string]*sessionState{}
	}
	ss, ok := s.Sessions[session]
	if !ok {
		ss = &sessionState{Rooms: map[string]roomMembership{}}
		s.Sessions[session] = ss
	}
	if ss.Rooms == nil {
		ss.Rooms = map[string]roomMembership{}
	}
	ss.Rooms[room] = roomMembership{Name: name, Mode: mode, Kind: kind, Joined: time.Now(), Repo: repo, Realm: realm}
	ss.LastRoom = room
}

// Leave removes room from session's joined-room set. If room was the
// session's most recent join, LastRoom is recomputed from whatever rooms
// remain (the one with the latest Joined timestamp), not simply cleared.
func (s *State) Leave(session, room string) {
	ss, ok := s.Sessions[session]
	if !ok {
		return
	}
	delete(ss.Rooms, room)
	if ss.LastRoom == room {
		ss.LastRoom = mostRecentRoom(ss.Rooms)
	}
}

func mostRecentRoom(rooms map[string]roomMembership) string {
	var latest string
	var latestAt time.Time
	for room, m := range rooms {
		if latest == "" || m.Joined.After(latestAt) {
			latest = room
			latestAt = m.Joined
		}
	}
	return latest
}

// LastRoom returns session's most recently joined room. ok is false when
// the session has not joined anything (either never seen, or left every
// room it joined).
func (s *State) LastRoom(session string) (room string, ok bool) {
	ss, exists := s.Sessions[session]
	if !exists || ss.LastRoom == "" {
		return "", false
	}
	return ss.LastRoom, true
}

// ResolveRoom picks the room a command should act on: explicit when given,
// else the session's last joined room, else a not-joined error carrying
// ExitNotJoined — the CLI's exit code 3, so a --room-less command outside
// any room fails distinctly rather than silently guessing.
func (s *State) ResolveRoom(session, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	room, ok := s.LastRoom(session)
	if !ok {
		return "", &Error{
			Code: ExitNotJoined,
			Msg:  "not joined any room; pass a room name or run `atomic bus join <room>` first",
		}
	}
	return room, nil
}
