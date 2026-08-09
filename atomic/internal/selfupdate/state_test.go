package selfupdate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
)

func TestLoadState_MissingFileYieldsZeroValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	got := selfupdate.LoadState(path)

	if got != (selfupdate.State{}) {
		t.Errorf("LoadState(missing) = %+v, want zero value", got)
	}
}

func TestLoadState_CorruptJSONYieldsZeroValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := selfupdate.LoadState(path)

	if got != (selfupdate.State{}) {
		t.Errorf("LoadState(corrupt) = %+v, want zero value", got)
	}
}

func TestLoadState_UnreadableFileYieldsZeroValue(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"update":{}}`), 0o000); err != nil {
		t.Fatal(err)
	}
	// Restore read permission so TempDir cleanup can remove it.
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	got := selfupdate.LoadState(path)

	if got != (selfupdate.State{}) {
		t.Errorf("LoadState(unreadable) = %+v, want zero value", got)
	}
}

func TestWriteState_RoundTripPreservesAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	want := selfupdate.State{
		Update: selfupdate.UpdateState{
			LastCheck:         time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
			Updating:          true,
			UpdateStartedAt:   time.Date(2026, 8, 9, 12, 1, 0, 0, time.UTC),
			UpdatedAt:         time.Date(2026, 8, 9, 12, 5, 0, 0, time.UTC),
			LastNotified:      time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC),
			LatestVersion:     "1.2.3",
			StageAttemptedFor: "1.2.3",
			LastResult:        "sha256 mismatch",
			Staged: selfupdate.StagedInfo{
				Version: "1.2.3",
				Path:    "/home/user/.cache/atomic/staged/atomic-1.2.3",
				SHA256:  "deadbeef",
			},
		},
	}

	if err := selfupdate.WriteState(path, want); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	got := selfupdate.LoadState(path)

	if !got.Update.LastCheck.Equal(want.Update.LastCheck) {
		t.Errorf("LastCheck = %v, want %v", got.Update.LastCheck, want.Update.LastCheck)
	}
	if got.Update.Updating != want.Update.Updating {
		t.Errorf("Updating = %v, want %v", got.Update.Updating, want.Update.Updating)
	}
	if !got.Update.UpdateStartedAt.Equal(want.Update.UpdateStartedAt) {
		t.Errorf("UpdateStartedAt = %v, want %v", got.Update.UpdateStartedAt, want.Update.UpdateStartedAt)
	}
	if !got.Update.UpdatedAt.Equal(want.Update.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.Update.UpdatedAt, want.Update.UpdatedAt)
	}
	if !got.Update.LastNotified.Equal(want.Update.LastNotified) {
		t.Errorf("LastNotified = %v, want %v", got.Update.LastNotified, want.Update.LastNotified)
	}
	if got.Update.LatestVersion != want.Update.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", got.Update.LatestVersion, want.Update.LatestVersion)
	}
	if got.Update.StageAttemptedFor != want.Update.StageAttemptedFor {
		t.Errorf("StageAttemptedFor = %q, want %q", got.Update.StageAttemptedFor, want.Update.StageAttemptedFor)
	}
	if got.Update.LastResult != want.Update.LastResult {
		t.Errorf("LastResult = %q, want %q", got.Update.LastResult, want.Update.LastResult)
	}
	if got.Update.Staged != want.Update.Staged {
		t.Errorf("Staged = %+v, want %+v", got.Update.Staged, want.Update.Staged)
	}
}

func TestWriteState_JSONFieldNamesSnakeCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s := selfupdate.State{
		Update: selfupdate.UpdateState{
			LatestVersion: "9.9.9",
			Staged:        selfupdate.StagedInfo{Version: "9.9.9", Path: "/x", SHA256: "abc"},
		},
	}
	if err := selfupdate.WriteState(path, s); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal top level: %v", err)
	}
	updateRaw, ok := top["update"]
	if !ok {
		t.Fatal(`missing top-level "update" key`)
	}

	var update map[string]json.RawMessage
	if err := json.Unmarshal(updateRaw, &update); err != nil {
		t.Fatalf("unmarshal update block: %v", err)
	}
	for _, key := range []string{
		"last_check", "updating", "update_started_at", "updated_at",
		"last_notified", "latest_version", "stage_attempted_for",
		"last_result", "staged",
	} {
		if _, ok := update[key]; !ok {
			t.Errorf("missing key %q in update block", key)
		}
	}

	var staged map[string]json.RawMessage
	if err := json.Unmarshal(update["staged"], &staged); err != nil {
		t.Fatalf("unmarshal staged block: %v", err)
	}
	for _, key := range []string{"version", "path", "sha256"} {
		if _, ok := staged[key]; !ok {
			t.Errorf("missing key %q in staged block", key)
		}
	}
}

func TestWriteState_LeavesNoTempFileResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := selfupdate.WriteState(path, selfupdate.State{}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory contents after WriteState = %v, want only [state.json]", names)
	}
}

func TestWriteState_RemovesLegacyCacheFileOpportunistically(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	legacyPath, err := selfupdate.DefaultCachePath()
	if err != nil {
		t.Fatalf("DefaultCachePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")
	if err := selfupdate.WriteState(statePath, selfupdate.State{}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy cache file still exists after WriteState (err=%v)", err)
	}
}

func TestWriteState_LegacyCacheAbsenceIsNotAnError(t *testing.T) {
	cacheHome := t.TempDir() // empty — no legacy file to clean up
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.json")

	if err := selfupdate.WriteState(statePath, selfupdate.State{}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
}
