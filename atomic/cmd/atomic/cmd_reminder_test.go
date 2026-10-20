package main

import (
	"io"
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

// A trailing --due must set the due date, not end up inside the reminder text.
func TestParseReminderAddArgs_TrailingFlagsHonored(t *testing.T) {
	opts, code := parseReminderAddArgs([]string{"check the build", "--due", "2026-11-01T09:00:00Z", "--transport", "cron"}, io.Discard)
	if code != 0 {
		t.Fatalf("got exit %d, want 0", code)
	}
	if opts.text != "check the build" {
		t.Errorf("text = %q, want %q", opts.text, "check the build")
	}
	if opts.due != "2026-11-01T09:00:00Z" {
		t.Errorf("due = %q, want trailing --due honored", opts.due)
	}
	if opts.transport != "cron" {
		t.Errorf("transport = %q, want trailing --transport honored", opts.transport)
	}
}

func TestParseReminderAddArgs_LeadingFlagsStillWork(t *testing.T) {
	opts, code := parseReminderAddArgs([]string{"--due", "2026-11-01T09:00:00Z", "check", "the", "build"}, io.Discard)
	if code != 0 {
		t.Fatalf("got exit %d, want 0", code)
	}
	if opts.text != "check the build" || opts.due != "2026-11-01T09:00:00Z" {
		t.Errorf("got text=%q due=%q", opts.text, opts.due)
	}
}

// After "--" a dash word is reminder text, so users can write "--due" literally.
func TestParseReminderAddArgs_TerminatorKeepsDashWordsInText(t *testing.T) {
	opts, code := parseReminderAddArgs([]string{"--", "remember", "--due", "x"}, io.Discard)
	if code != 0 {
		t.Fatalf("got exit %d, want 0", code)
	}
	if opts.text != "remember --due x" || opts.due != "" {
		t.Errorf("got text=%q due=%q, want text %q and no due", opts.text, opts.due, "remember --due x")
	}
}

func TestParseReminderAddArgs_ErrorsExitOne(t *testing.T) {
	if _, code := parseReminderAddArgs([]string{"--due", "2026-11-01T09:00:00Z"}, io.Discard); code != 1 {
		t.Errorf("missing text: got exit %d, want 1", code)
	}
	if _, code := parseReminderAddArgs([]string{"text", "--bogus"}, io.Discard); code != 1 {
		t.Errorf("unknown flag: got exit %d, want 1", code)
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
