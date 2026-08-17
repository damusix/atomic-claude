package bus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sessionEnvVar is the environment variable a live Claude Code session sets to a
// UUID identifying that session.
const sessionEnvVar = "CLAUDE_CODE_SESSION_ID"

// SessionID identifies the current agent. Identity is keyed by session id, never
// cwd or pid: two Claude Code sessions can run in the same directory and are
// still two distinct agents. override is the --session flag, for scripted use
// and tests outside a live session.
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

// roomMembership is one session's record of a joined room: the name it ended up
// with (a join may be renamed by the daemon's numeric-suffix retry), its kind
// and mode, and when. Mode and Kind are persisted so Hub.Rehydrate can restore a
// member exactly as it joined — held only in daemon memory, a restart silently
// reset every observer back to participate.
type roomMembership struct {
	Name   string    `json:"name"`
	Mode   string    `json:"mode,omitempty"`
	Kind   string    `json:"kind,omitempty"`
	Joined time.Time `json:"joined"`

	// Repo and Realm are persisted for the same reason Mode and Kind are: so
	// Hub.Rehydrate restores position exactly as Hub.Join recorded it.
	Repo  string `json:"repo,omitempty"`
	Realm string `json:"realm,omitempty"`

	// LastSeen is the persisted counterpart to Hub.Publish's in-memory refresh.
	// Without it Hub.Rehydrate had only "now" to stamp on restart, resurrecting a
	// member dead for hours as freshly live and putting it out of prune's reach.
	// Zero on a bus.json written before this field; Rehydrate falls back to Joined.
	LastSeen time.Time `json:"last_seen"`
}

// roomState is one room's operator-controlled state, persisted independently of
// any session's membership (a room can be halted with zero members). Halt lived
// only in daemon memory before this, so a restart silently released it.
type roomState struct {
	Halted   bool   `json:"halted,omitempty"`
	HaltText string `json:"halt_text,omitempty"`
}

// sessionState is one session's bus.json entry.
type sessionState struct {
	Rooms map[string]roomMembership `json:"rooms"`
	// LastRoom defaults a --room-less command. Recomputed on Leave rather than
	// cleared, so leaving the most recent room still leaves a sensible default
	// behind when other rooms remain joined.
	LastRoom string `json:"last_room,omitempty"`
}

// State is the per-session joined-room map persisted at ~/.atomic/bus.json.
// Every CLI invocation is a fresh process; this file — not process memory, not
// the daemon — is what remembers what a session has joined between invocations.
type State struct {
	Sessions map[string]*sessionState `json:"sessions"`

	// Rooms holds room-level state outliving any one membership — currently only
	// halt. Omitted from the wire when nothing is halted, so an ordinary bus.json
	// stays exactly as small as before this field existed.
	Rooms map[string]*roomState `json:"rooms,omitempty"`
}

// Load reads State from <home>/.atomic/bus.json. A missing file is not an error
// — it means no session has ever joined a room — and yields an empty State.
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

// Save writes State to <home>/.atomic/bus.json via write-to-tmp + rename for
// interrupt safety, matching config.WritePersist's pattern.
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

// Join records that session joined room under name and marks it the session's
// most recent join. LastSeen starts equal to Joined — a just-joined member is by
// definition not stale yet.
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
	now := time.Now()
	ss.Rooms[room] = roomMembership{Name: name, Mode: mode, Kind: kind, Joined: now, LastSeen: now, Repo: repo, Realm: realm}
	ss.LastRoom = room
}

// TouchLastSeen records that session was active in room at now — the persisted
// counterpart to Hub.Publish's in-memory refresh. Reports whether there was a
// membership to touch; a caller racing a concurrent leave has nothing to update.
func (s *State) TouchLastSeen(session, room string, now time.Time) bool {
	ss, ok := s.Sessions[session]
	if !ok {
		return false
	}
	m, ok := ss.Rooms[room]
	if !ok {
		return false
	}
	m.LastSeen = now
	ss.Rooms[room] = m
	return true
}

// SetHalted records or clears room's halt flag — the persisted counterpart to
// Hub.Halt/Hub.Resume, which only mutate the daemon's in-memory Room. Resuming
// deletes the entry rather than storing Halted:false: absent and resumed mean
// the same thing, and a history of every room ever halted is worth nothing.
func (s *State) SetHalted(room string, halted bool, text string) {
	if !halted {
		delete(s.Rooms, room)
		return
	}
	if s.Rooms == nil {
		s.Rooms = map[string]*roomState{}
	}
	s.Rooms[room] = &roomState{Halted: true, HaltText: text}
}

// ClearRoom removes room from every session's persisted membership and clears
// its halt state — the bus.json-side half of Hub.Close. Unlike Leave it mutates
// other sessions' state, the same operator authority Hub.Close already has to
// evict every member's live roster entry.
func (s *State) ClearRoom(room string) {
	for _, ss := range s.Sessions {
		if _, ok := ss.Rooms[room]; !ok {
			continue
		}
		delete(ss.Rooms, room)
		if ss.LastRoom == room {
			ss.LastRoom = mostRecentRoom(ss.Rooms)
		}
	}
	delete(s.Rooms, room)
}

// Leave removes room from session's joined-room set. LastRoom is recomputed from
// whatever rooms remain (latest Joined), not simply cleared.
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

// LastRoom returns session's most recently joined room. ok is false when the
// session has joined nothing, or has left every room it joined.
func (s *State) LastRoom(session string) (room string, ok bool) {
	ss, exists := s.Sessions[session]
	if !exists || ss.LastRoom == "" {
		return "", false
	}
	return ss.LastRoom, true
}

// ResolveRoom picks the room a command acts on: explicit when given, else the
// session's last joined room, else an ExitNotJoined error — so a --room-less
// command outside any room fails distinctly rather than silently guessing.
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
