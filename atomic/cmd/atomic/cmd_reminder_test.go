package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// remindersPath returns the path to the reminders directory used by the CLI
// dispatch. Mirrors the constant in the reminder package so this test breaks
// loudly if the path ever changes.
func remindersPath(root string) string {
	return filepath.Join(root, ".claude", ".scratchpad", "reminders")
}

// TestReminderSetDueCLIWiring exercises the set-due dispatch path end-to-end:
// add a reminder via the same package function runReminder calls, then invoke
// SetDue (also called directly by runReminder), and assert the on-disk file
// has only the due: field changed while id, created, transport, and body are
// untouched.
func TestReminderSetDueCLIWiring(t *testing.T) {
	root := t.TempDir()

	const body = "deploy the staging release"
	const transport = "cron"
	const origDue = "2026-05-20T09:00:00Z"
	const newDue = "2026-06-01T12:00:00Z"

	// Add a reminder with an initial due and transport — mirrors what
	// `atomic reminder add --due <iso> --transport <kind> <text>` dispatches to.
	id, err := reminder.Add(root, body, reminder.WithDue(origDue), reminder.WithTransport(transport))
	if err != nil {
		t.Fatalf("reminder.Add: %v", err)
	}

	// Invoke SetDue — exactly what runReminder dispatches for "set-due".
	if err := reminder.SetDue(root, id, newDue); err != nil {
		t.Fatalf("reminder.SetDue: %v", err)
	}

	// Read the on-disk file and assert field-by-field.
	entries, err := os.ReadDir(remindersPath(root))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 reminder file, got %d", len(entries))
	}
	raw, err := os.ReadFile(filepath.Join(remindersPath(root), entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)

	if !strings.Contains(content, "due: "+newDue) {
		t.Errorf("expected due field %q in file; got:\n%s", newDue, content)
	}
	if strings.Contains(content, "due: "+origDue) {
		t.Errorf("old due %q should be gone; got:\n%s", origDue, content)
	}
	if !strings.Contains(content, "id: "+id) {
		t.Errorf("id field %q missing after SetDue; got:\n%s", id, content)
	}
	if !strings.Contains(content, "transport: "+transport) {
		t.Errorf("transport field %q missing after SetDue; got:\n%s", transport, content)
	}
	if !strings.Contains(content, body) {
		t.Errorf("body %q missing after SetDue; got:\n%s", body, content)
	}
}

// TestReminderSetDueErrorPaths exercises the error branches that runReminder
// propagates to stderr+exit(1) for set-due.
func TestReminderSetDueErrorPaths(t *testing.T) {
	root := t.TempDir()

	// Unknown id — no reminder file exists.
	err := reminder.SetDue(root, "r-nonexistent", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for unknown id, got nil")
	}
	if !strings.Contains(err.Error(), "no reminder with id") {
		t.Errorf("expected 'no reminder with id' in error; got: %v", err)
	}

	// Valid id but malformed ISO timestamp.
	id, err := reminder.Add(root, "check the dashboard")
	if err != nil {
		t.Fatalf("reminder.Add: %v", err)
	}
	err = reminder.SetDue(root, id, "not-a-timestamp")
	if err == nil {
		t.Fatal("expected error for malformed ISO, got nil")
	}
	if !strings.Contains(err.Error(), "must be RFC3339") {
		t.Errorf("expected 'must be RFC3339' in error; got: %v", err)
	}

	// Missing args: simulated by calling SetDue with empty id.
	err = reminder.SetDue(root, "", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}
