package retro_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/retro"
)

func writeRunFile(t *testing.T, dir, name, runTS string) {
	t.Helper()
	content := `{"run_ts":"` + runTS + `"}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLastRunSince_NewestByName(t *testing.T) {
	dir := t.TempDir()
	writeRunFile(t, dir, "2026-08-01T00-00-00.json", "2026-08-01T00:00:00Z")
	writeRunFile(t, dir, "2026-09-19T12-00-00.json", "2026-09-19T12:00:00Z")

	ts, ok, err := retro.LastRunSince(dir)
	if err != nil {
		t.Fatalf("LastRunSince: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("ts = %v, want %v", ts, want)
	}
}

func TestLastRunSince_MissingDirReportsAbsent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	_, ok, err := retro.LastRunSince(dir)
	if err != nil {
		t.Fatalf("LastRunSince: %v", err)
	}
	if ok {
		t.Error("expected ok=false for a missing dir")
	}
}

func TestLastRunSince_EmptyDirReportsAbsent(t *testing.T) {
	dir := t.TempDir()

	_, ok, err := retro.LastRunSince(dir)
	if err != nil {
		t.Fatalf("LastRunSince: %v", err)
	}
	if ok {
		t.Error("expected ok=false for a dir with no *.json file")
	}
}
