package selfupdate

import (
	"os"
	"path/filepath"
)

// DefaultCachePath locates the legacy update cache file, honoring
// XDG_CACHE_HOME. state.json supersedes it; this survives only so WriteState
// can delete the leftover.
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
