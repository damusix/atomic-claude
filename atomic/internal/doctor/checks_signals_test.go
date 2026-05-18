package doctor_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeSignalsFile creates .claude/project/deterministic-signals.md in root
// and sets its mtime to the given time.
func makeSignalsFile(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	path := filepath.Join(dir, "deterministic-signals.md")
	if err := os.WriteFile(path, []byte("# Deterministic signals\n"), 0o644); err != nil {
		t.Fatalf("write signals file: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

// TestCheckSignalsMissingFile verifies WARN when signals file does not exist.
func TestCheckSignalsMissingFile(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckSignalsFreshFile verifies PASS when signals file is fresh (below threshold).
func TestCheckSignalsFreshFile(t *testing.T) {
	root := t.TempDir()
	// mtime = 3 days ago, threshold = 7
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

// TestCheckSignalsStaleByAge verifies WARN when signals file is older than --stale-days.
func TestCheckSignalsStaleByAge(t *testing.T) {
	root := t.TempDir()
	// mtime = 10 days ago, threshold = 7
	mtime := time.Now().Add(-10 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)
	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	// Detail must mention the age in days and threshold.
	if r.Detail == "" {
		t.Error("Detail is empty")
	}
}

// TestCheckSignalsStaleDaysOverride verifies --stale-days is respected.
// A file 3 days old is PASS at threshold=7 but WARN at threshold=2.
func TestCheckSignalsStaleDaysOverride(t *testing.T) {
	root := t.TempDir()
	mtime := time.Now().Add(-3 * 24 * time.Hour)
	makeSignalsFile(t, root, mtime)

	r7 := doctor.RunCheckSignalsWith(root, 7)
	if r7.Severity != doctor.PASS {
		t.Errorf("at threshold=7: severity = %v, want PASS", r7.Severity)
	}

	r2 := doctor.RunCheckSignalsWith(root, 2)
	if r2.Severity != doctor.WARN {
		t.Errorf("at threshold=2: severity = %v, want WARN", r2.Severity)
	}
}

// TestCheckSignalsSourceNewerThanFile verifies WARN when a source file was
// modified after the signals file (ErrStale path).
// We create the signals file first, then touch a new source file after.
func TestCheckSignalsSourceNewerThanFile(t *testing.T) {
	root := t.TempDir()
	// Signals file mtime = 2 days ago (fresh by age).
	signalsMtime := time.Now().Add(-2 * 24 * time.Hour)
	makeSignalsFile(t, root, signalsMtime)

	// Create a source file (e.g., CLAUDE.md) with current mtime (newer than signals).
	srcPath := filepath.Join(root, "CLAUDE.md")
	if err := os.WriteFile(srcPath, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	// Mtime is already now, which is newer than signalsMtime.

	r := doctor.RunCheckSignalsWith(root, 7)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN (ErrStale path)", r.Severity)
	}
}
