package bus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// appendMu serializes writes to room log files across the process. In
// practice every Append call reaches here through Hub.Publish, which
// already serializes under Hub.mu — this mutex exists so Append stays
// correct on its own regardless of caller, rather than relying on a
// synchronization guarantee this file can't see or enforce.
var appendMu sync.Mutex

// scannerFlatOverheadBytes is headroom for everything a persisted envelope
// costs beyond the four fields that scale with a Publish-admitted cap
// (Text, Room, From, ReplyTo, To — see scannerMaxLineBytes below): JSON's
// quotes/keys/colons/commas/braces, plus id (a base36-encoded uint64, at
// most 13 ASCII digits — its alphabet never needs \u00XX escaping),
// from_kind (fixed "agent" or "human", Hub.Join's validKind closes off
// anything else), and ts (time.Time's RFC3339Nano encoding, at most 35
// characters). None of those three scales with a cap or needs the 6x
// escape factor, so they don't belong under it. The true worst case for
// all of that, computed against the current Envelope shape, is under 200
// bytes; this constant is set well above that so a future field addition —
// or a small miscount in this derivation — stays safe without forcing a
// re-derivation to the byte. Truncated and Log are always omitted on a
// persisted envelope (roomlog.go's Append call sites never set either —
// the one call site that does, dropMarkerEnvelope, is documented never to
// reach Append), so neither costs anything here.
const scannerFlatOverheadBytes = 8192

// scannerMaxLineBytes bounds a single room-log line ReadSince will accept.
// It must clear the worst-case JSON size of a Publish-admitted envelope, or
// a message Publish legitimately accepted could still overflow this
// scanner and break ReadSince for that room forever — exactly the failure
// MaxTextBytes exists to close off.
//
// Every string-typed field capped directly by Hub.Publish/Hub.Join — Text
// (MaxTextBytes), Room/From/ReplyTo (MaxIdentifierBytes each, three
// fields), and To (MaxAddressees entries totaling MaxAddresseesBytes
// combined raw length) — can independently hit its cap with every raw byte
// being one encoding/json escapes to a 6-byte \u00XX sequence, the worst
// case that factor accounts for. scannerFlatOverheadBytes above covers
// everything else (see its own comment).
//
// TestAppend_ReadSince_EnvelopeAtEveryMetadataLimitRoundTrips (room_test.go)
// is the actual proof: it fills every capped field with an escaping byte —
// not merely a same-length ASCII one — and asserts the resulting
// worst-case envelope round-trips.
const scannerMaxLineBytes = (MaxTextBytes+3*MaxIdentifierBytes+MaxAddresseesBytes)*6 + scannerFlatOverheadBytes

// Append writes one JSON line for env to
// <home>/.atomic/rooms/<room>.log (RoomLogPath), creating the file and its
// parent directory if absent. Every room's traffic is logged
// unconditionally, whether or not anyone is subscribed — this file is the
// operator's only way to reconstruct a loop that ran overnight.
func Append(home, room string, env Envelope) error {
	path := RoomLogPath(home, room)

	appendMu.Lock()
	defer appendMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("bus: mkdir %s: %w", filepath.Dir(path), err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("bus: open room log %s: %w", path, err)
	}
	defer f.Close()

	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("bus: marshal envelope: %w", err)
	}
	b = append(b, '\n')

	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("bus: write room log %s: %w", path, err)
	}
	return nil
}

// ReadSince reads every envelope in room's log after the one whose id is
// sinceID, or every envelope if sinceID is empty or not found. This is the
// disk-backed recovery path for a daemon that just restarted — the ring
// Hub.Since serves is in-memory and does not survive a restart, but this
// file does. A missing log file is not an error: it means the room has
// never had a message, and yields an empty, nil-error result.
func ReadSince(home, room, sinceID string) ([]Envelope, error) {
	path := RoomLogPath(home, room)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("bus: open room log %s: %w", path, err)
	}
	defer f.Close()

	var all []Envelope
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), scannerMaxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var env Envelope
		if err := json.Unmarshal(line, &env); err != nil {
			return nil, fmt.Errorf("bus: parse room log %s: %w", path, err)
		}
		all = append(all, env)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("bus: read room log %s: %w", path, err)
	}

	if sinceID == "" {
		return all, nil
	}
	for i, env := range all {
		if env.ID == sinceID {
			return append([]Envelope{}, all[i+1:]...), nil
		}
	}
	return all, nil
}
