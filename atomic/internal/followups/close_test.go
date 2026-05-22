package followups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClose_HappyPath(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	// Create an entry to close.
	if _, err := Add(dir, AddOpts{
		ID: "close-me", Title: "Something to close", Severity: "risk", Origin: "origin", Today: today,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := CloseEntry(dir, "close-me", "", today); err != nil {
		t.Fatalf("CloseEntry: %v", err)
	}

	// Entry file should be deleted.
	if _, err := os.Stat(filepath.Join(dir, "close-me.md")); err == nil {
		t.Error("expected close-me.md to be deleted, still exists")
	}

	// CLOSED.md should have a line for close-me.
	closedRaw, err := os.ReadFile(filepath.Join(dir, "CLOSED.md"))
	if err != nil {
		t.Fatalf("CLOSED.md: %v", err)
	}
	if !strings.Contains(string(closedRaw), "close-me") {
		t.Errorf("CLOSED.md should contain 'close-me', got:\n%s", string(closedRaw))
	}
	// Default marker format includes the date.
	if !strings.Contains(string(closedRaw), "2026-05-22") {
		t.Errorf("CLOSED.md should contain the date, got:\n%s", string(closedRaw))
	}
}

func TestClose_WithReason(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	if _, err := Add(dir, AddOpts{
		ID: "with-reason", Title: "Entry with reason", Severity: "nit", Origin: "origin", Today: today,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := CloseEntry(dir, "with-reason", "fixed in commit abc123", today); err != nil {
		t.Fatalf("CloseEntry: %v", err)
	}

	closedRaw, _ := os.ReadFile(filepath.Join(dir, "CLOSED.md"))
	if !strings.Contains(string(closedRaw), "fixed in commit abc123") {
		t.Errorf("CLOSED.md should contain custom reason, got:\n%s", string(closedRaw))
	}
}

func TestClose_IDMissing(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	err := CloseEntry(dir, "nonexistent", "", today)
	if err == nil {
		t.Fatal("expected error for nonexistent id, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention id, got: %v", err)
	}
}

func TestClose_RegeneratesIndex(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude", "project", "followups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	today := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)

	// Create two entries.
	if _, err := Add(dir, AddOpts{ID: "keep-me", Title: "Keep", Severity: "risk", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add keep-me: %v", err)
	}
	if _, err := Add(dir, AddOpts{ID: "close-me2", Title: "Close", Severity: "nit", Origin: "o", Today: today}); err != nil {
		t.Fatalf("Add close-me2: %v", err)
	}

	if err := CloseEntry(dir, "close-me2", "", today); err != nil {
		t.Fatalf("CloseEntry: %v", err)
	}

	// INDEX.md should exist and mention keep-me but not close-me2.
	indexRaw, err := os.ReadFile(filepath.Join(dir, "INDEX.md"))
	if err != nil {
		t.Fatalf("INDEX.md: %v", err)
	}
	if !strings.Contains(string(indexRaw), "keep-me") {
		t.Errorf("INDEX.md should still contain keep-me")
	}
	if strings.Contains(string(indexRaw), "close-me2") {
		t.Errorf("INDEX.md should not contain closed close-me2")
	}
}
