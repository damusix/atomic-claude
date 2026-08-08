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

// scannerMaxLineBytes bounds one log line for readers of this file: Text
// can be MaxTextBytes, plus headroom for the envelope's bounded metadata
// fields (MaxIdentifierBytes ids/names, MaxAddresseesBytes addressees,
// JSON syntax overhead).
const scannerMaxLineBytes = MaxTextBytes + 64*1024

// ReadEnvelope scans room's log for the envelope with the given id. Found
// is false when the log exists but holds no such id; a missing log file
// returns os.ErrNotExist (the room has never had traffic). Later
// occurrences win on a duplicate id, matching the append order — though
// ids are unique across daemon restarts by construction (see Envelope.ID).
func ReadEnvelope(home, room, id string) (Envelope, bool, error) {
	f, err := os.Open(RoomLogPath(home, room))
	if err != nil {
		return Envelope{}, false, err
	}
	defer f.Close()

	var found Envelope
	ok := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), scannerMaxLineBytes)
	for scanner.Scan() {
		var env Envelope
		if json.Unmarshal(scanner.Bytes(), &env) != nil {
			continue
		}
		if env.ID == id {
			found, ok = env, true
		}
	}
	if err := scanner.Err(); err != nil {
		return Envelope{}, false, fmt.Errorf("bus: scan room log: %w", err)
	}
	return found, ok, nil
}
