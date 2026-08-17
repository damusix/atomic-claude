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

// LockPath returns <home>/.atomic/bus.lock — the flock guarding the daemon
// spawn-and-connect sequence, so two cold `join` calls produce one daemon.
func LockPath(home string) string {
	return filepath.Join(config.Dir(home), "bus.lock")
}

// StatePath returns <home>/.atomic/bus.json — the per-session joined-room
// state (see State in identity.go).
func StatePath(home string) string {
	return filepath.Join(config.Dir(home), "bus.json")
}

// RoomLogPath returns <home>/.atomic/rooms/<room>.log — the durable append-only
// record of a room's traffic. There is no replay buffer; this file is the only
// history.
func RoomLogPath(home, room string) string {
	return filepath.Join(roomsDir(home), room+".log")
}

// EnsureDirs creates <home>/.atomic/rooms at 0700. Unix file permissions are
// bus's only authentication, so room logs stay private to this user.
func EnsureDirs(home string) error {
	return os.MkdirAll(roomsDir(home), 0o700)
}

func roomsDir(home string) string {
	return filepath.Join(config.Dir(home), "rooms")
}
