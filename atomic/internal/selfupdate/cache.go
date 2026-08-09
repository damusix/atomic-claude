package selfupdate

import (
	"os"
	"path/filepath"
)

// DefaultCachePath returns the path to the legacy update cache file.
// Respects XDG_CACHE_HOME if set, otherwise uses ~/.cache/atomic/update.json.
// The cache itself is superseded by state.json (see state.go); this helper
// survives solely so WriteState can opportunistically delete the legacy file.
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
