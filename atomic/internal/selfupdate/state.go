package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State is the on-disk shape of ~/.atomic/state.json — the single
// machine-managed source of truth for update-check cadence, staged
// downloads, and swap-lock coordination. No atomic invocation performs
// network I/O directly; the update-available banner and all update
// decisions read from this file alone.
type State struct {
	Update UpdateState `json:"update"`
}

// UpdateState tracks the self-update lifecycle: check cadence, banner
// dedup, the swap lock, and the once-only staging budget for a version.
type UpdateState struct {
	LastCheck         time.Time  `json:"last_check"`
	Updating          bool       `json:"updating"`
	UpdateStartedAt   time.Time  `json:"update_started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastNotified      time.Time  `json:"last_notified"`
	LatestVersion     string     `json:"latest_version"`
	StageAttemptedFor string     `json:"stage_attempted_for"`
	LastResult        string     `json:"last_result"`
	Staged            StagedInfo `json:"staged"`
}

// StagedInfo records a downloaded-and-checksum-verified binary awaiting an
// `atomic update` swap.
type StagedInfo struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

// LoadState reads state.json at path. A missing file, corrupt JSON, or an
// unreadable file all yield a zero-value State — never an error — since a
// state-read failure must never block the invoked verb.
func LoadState(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}
	}
	return s
}

// WriteState writes state atomically (temp file + rename, same directory as
// path to guarantee same filesystem), then opportunistically removes the
// legacy ~/.cache/atomic/update.json cache file — its absence, or any
// failure locating/removing it, is never an error; nothing reads that file
// once state.json exists.
func WriteState(path string, s State) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("selfupdate: mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("selfupdate: marshal state: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("selfupdate: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: close temp: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: rename to %s: %w", path, err)
	}

	removeLegacyCache()

	return nil
}

// removeLegacyCache opportunistically deletes the pre-state.json cache
// file. Its absence, or any failure locating or removing it, is never an
// error.
func removeLegacyCache() {
	path, err := DefaultCachePath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
