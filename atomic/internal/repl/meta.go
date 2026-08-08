package repl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Meta is what the CLI records about a running session, so a later invocation
// — a separate process with none of the first one's memory — can find, report
// on, and signal it.
//
// There is deliberately no field for the environment a session was started
// with: `list` and `status` render this struct, and an --env value must never
// appear in their output. Nowhere to put one is a stronger guarantee than
// remembering to filter.
type Meta struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
	Bin  string `json:"bin"`
	PID  int    `json:"pid"`

	Socket string `json:"socket"`

	// StartedAt is what makes PID safe to signal. A pid on its own is a
	// recycled-pid hazard: by the time an eval times out, the number may
	// belong to something else entirely, so the escalation cross-checks this
	// against the live process before sending anything.
	StartedAt time.Time `json:"started_at"`

	// Root is the scope root the session keys to — its harness's working
	// directory, and what `list --all` prints so a session found from another
	// repo is identifiable.
	Root string `json:"root"`
}

// LoadMeta reads a session's meta file. An absent file wraps os.ErrNotExist so
// callers can tell "no session by that name" (which is a routine answer) from a
// read failure (which is not).
func LoadMeta(path string) (Meta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, fmt.Errorf("repl: read session meta %s: %w", path, err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Meta{}, fmt.Errorf("repl: parse session meta %s: %w", path, err)
	}
	return meta, nil
}

// Save writes the meta file at 0600 through a temp file in the same directory,
// so a reader never observes a half-written record and a re-`start` never
// leaves the previous pid behind. The parent directory is created 0700 —
// the file names a pid the CLI will signal.
func (m Meta) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("repl: create session dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("repl: encode session meta: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".meta-*.tmp")
	if err != nil {
		return fmt.Errorf("repl: create temp meta in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("repl: chmod temp meta %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("repl: write temp meta %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("repl: close temp meta %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("repl: install session meta %s: %w", path, err)
	}
	return nil
}
