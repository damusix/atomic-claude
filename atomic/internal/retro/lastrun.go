package retro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LastRunSince reads run_ts from the newest (by filename) *.json file in dir,
// which is $HOME/.atomic/retro-runs at the CLI layer. ok is false when dir is
// absent or holds no *.json file, in which case the caller falls back to a
// 30-day default.
func LastRunSince(dir string) (time.Time, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return time.Time{}, false, nil
	}
	sort.Strings(names)
	newest := names[len(names)-1]

	data, err := os.ReadFile(filepath.Join(dir, newest))
	if err != nil {
		return time.Time{}, false, err
	}
	var payload struct {
		RunTS string `json:"run_ts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}, false, err
	}
	ts, err := time.Parse(time.RFC3339, payload.RunTS)
	if err != nil {
		return time.Time{}, false, err
	}
	return ts, true, nil
}
