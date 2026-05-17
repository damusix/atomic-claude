package selfupdate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// CacheEntry is the on-disk schema for ~/.cache/atomic/update.json.
type CacheEntry struct {
	CheckedAt      time.Time `json:"checked_at"`
	CurrentVersion string    `json:"current_version"`
	LatestVersion  string    `json:"latest_version"`
	NotifiedAt     time.Time `json:"notified_at"`
}

// DefaultCachePath returns the path to the update cache file.
// Respects XDG_CACHE_HOME if set, otherwise uses ~/.cache/atomic/update.json.
func DefaultCachePath() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "atomic", "update.json"), nil
}

// ReadCache reads the cache file. Returns a zero-value CacheEntry (not an
// error) if the file does not exist.
func ReadCache(path string) (CacheEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CacheEntry{}, nil
		}
		return CacheEntry{}, err
	}
	var e CacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return CacheEntry{}, err
	}
	return e, nil
}

// WriteCache writes the cache entry as pretty-printed JSON.
func WriteCache(path string, e CacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	// write to temp then rename for atomicity
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
