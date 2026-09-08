package gateway

import (
	"encoding/hex"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus/remote"
)

// AdmissionWindow is the timestamp and nonce-retention window: 120 seconds
// either side of gateway time. Equal to remote.AdmissionWindow, which checks
// the same window on the client's s2c side — see that constant's doc comment
// for why they can't share one definition.
const AdmissionWindow = remote.AdmissionWindow

// Window tracks the admissible timestamp range and the seen-nonce set that
// together reject a replayed frame. Its low edge is max(now-window, start):
// a bare now-window edge would let a frame captured just before a gateway
// restart back into the admissible range once the in-memory seen set has
// been dropped, so start closes that gap.
type Window struct {
	window time.Duration
	start  time.Time

	mu   sync.Mutex
	seen map[string]int64 // "<key id>:<hex nonce>" -> frame timestamp, unix seconds
}

// NewWindow returns a Window spanning window seconds either side of now,
// whose low edge never reaches earlier than start. start is truncated to
// the second: a frame's Header.Timestamp is always Unix-second-truncated,
// so an untruncated start could sit ahead of it and reject an admissible
// frame sealed in the same wall-clock second Window was constructed in.
func NewWindow(window time.Duration, start time.Time) *Window {
	return &Window{window: window, start: start.Truncate(time.Second), seen: make(map[string]int64)}
}

func (w *Window) lowEdge(now time.Time) time.Time {
	low := now.Add(-w.window)
	if w.start.After(low) {
		return w.start
	}
	return low
}

// Admissible reports whether ts falls within the window around now, with the
// low edge raised to start; see Window's doc comment for why.
func (w *Window) Admissible(ts, now time.Time) bool {
	if ts.Before(w.lowEdge(now)) {
		return false
	}
	return !ts.After(now.Add(w.window))
}

// Admit reports whether (keyID, nonce) is a first sighting, and records it
// if so. Callers must check Admissible first: Admit does not re-validate ts.
// Entries whose timestamp has fallen below the current low edge are swept on
// every call — a frame that old is no longer Admissible either, so nothing
// depends on the swept entry to catch a replay; sweeping any earlier would
// reopen the replay window this exists to close.
func (w *Window) Admit(keyID string, nonce [12]byte, ts, now time.Time) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	low := w.lowEdge(now).Unix()
	for k, seenAt := range w.seen {
		if seenAt < low {
			delete(w.seen, k)
		}
	}

	key := keyID + ":" + hex.EncodeToString(nonce[:])
	if _, ok := w.seen[key]; ok {
		return false
	}
	w.seen[key] = ts.Unix()
	return true
}
