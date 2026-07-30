package bus

import (
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// SocketPath returns <home>/.atomic/bus.sock — the daemon's Unix domain
// socket.
func SocketPath(home string) string {
	return filepath.Join(config.Dir(home), "bus.sock")
}

// LockPath returns <home>/.atomic/bus.lock — the flock that guards the
// daemon spawn-and-connect sequence, so two concurrent `join` calls racing
// from cold produce exactly one daemon.
func LockPath(home string) string {
	return filepath.Join(config.Dir(home), "bus.lock")
}

// StatePath returns <home>/.atomic/bus.json — the per-session joined-room
// state (see State in identity.go).
func StatePath(home string) string {
	return filepath.Join(config.Dir(home), "bus.json")
}

// RoomLogPath returns <home>/.atomic/rooms/<room>.log — the durable,
// append-only record of a room's traffic. There is no replay of any kind
// (docs/spec/atomic-bus.md's Non-goals); this file is the only history.
func RoomLogPath(home, room string) string {
	return filepath.Join(roomsDir(home), room+".log")
}

// EnsureDirs creates <home>/.atomic/rooms (and its parent) at 0700, if
// absent. 0700 keeps room logs private to this user — bus's only
// authentication is Unix file permissions (see docs/design/atomic-bus.md).
func EnsureDirs(home string) error {
	return os.MkdirAll(roomsDir(home), 0o700)
}

func roomsDir(home string) string {
	return filepath.Join(config.Dir(home), "rooms")
}
