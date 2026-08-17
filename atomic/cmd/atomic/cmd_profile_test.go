package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Callers rely on exit 2 to tell a usage error from a runtime error.
func TestProfileAction_NoArgsUsageError(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(no args): got exit code %d, want 2", code)
	}
}

func TestProfileAction_UnknownVerbUsageError(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"bogus"}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(bogus): got exit code %d, want 2", code)
	}
}

// The profile package unit-tests Refresh itself; this covers the dispatch
// wiring that reaches it.
func TestProfileAction_RefreshWritesFile(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"refresh"}, home, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh): got exit code %d, want 0", code)
	}

	profilePath := filepath.Join(home, ".atomic", "profile.md")
	content, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("profile.md not written: %v", err)
	}
	if !strings.Contains(string(content), "<deterministic lastcheck=2026-05-28>") {
		t.Errorf("profile.md missing lastcheck stamp; got:\n%s", string(content))
	}
}

// A bad duration is a runtime error (exit 1), not a usage error (exit 2).
func TestProfileAction_IfStaleBadDuration(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"refresh", "--if-stale", "7h"}, home, "2026-05-28")
	if code != 1 {
		t.Errorf("profileAction(refresh --if-stale 7h): got exit code %d, want 1", code)
	}
}

// The --if-stale gate exists to avoid spurious re-runs at session start.
func TestProfileAction_IfStaleNoOpWhenFresh(t *testing.T) {
	home := t.TempDir()
	atomicDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# User profile\n\n## Environment\n<deterministic lastcheck=2026-05-28>\n- OS: darwin\n</deterministic>\n"
	profilePath := filepath.Join(atomicDir, "profile.md")
	if err := os.WriteFile(profilePath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	statBefore, _ := os.Stat(profilePath)

	code := profileAction([]string{"refresh", "--if-stale", "7d"}, home, "2026-05-28")
	if code != 0 {
		t.Fatalf("profileAction(refresh --if-stale 7d) fresh: got exit code %d, want 0", code)
	}

	statAfter, _ := os.Stat(profilePath)
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Error("profileAction: file mtime changed even though lastcheck was fresh")
	}
}
