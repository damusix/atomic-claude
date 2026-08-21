package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/reminder"
)

// remindersPath mirrors reminder.Add's write target: the project-keyed home.
func remindersPath(root string) string {
	return config.ProjectRemindersDir(root)
}

// Only due: may change; id, created, transport and body must survive SetDue.
func TestReminderSetDueCLIWiring(t *testing.T) {
	root := t.TempDir()

	const body = "deploy the staging release"
	const transport = "cron"
	const origDue = "2026-05-20T09:00:00Z"
	const newDue = "2026-06-01T12:00:00Z"

	id, err := reminder.Add(root, body, reminder.WithDue(origDue), reminder.WithTransport(transport))
	if err != nil {
		t.Fatalf("reminder.Add: %v", err)
	}

	if err := reminder.SetDue(root, id, newDue); err != nil {
		t.Fatalf("reminder.SetDue: %v", err)
	}

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

// The branches runReminder propagates to stderr plus exit 1.
func TestReminderSetDueErrorPaths(t *testing.T) {
	root := t.TempDir()

	err := reminder.SetDue(root, "r-nonexistent", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for unknown id, got nil")
	}
	if !strings.Contains(err.Error(), "no reminder with id") {
		t.Errorf("expected 'no reminder with id' in error; got: %v", err)
	}

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

	// Missing args, simulated with an empty id.
	err = reminder.SetDue(root, "", "2026-06-01T12:00:00Z")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}
