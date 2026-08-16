package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileAction_NoArgsUsageError verifies that profileAction with no args
// returns exit code 2 (usage error). WHY: callers rely on exit 2 to distinguish
// usage errors from runtime errors.
func TestProfileAction_NoArgsUsageError(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(no args): got exit code %d, want 2", code)
	}
}

// TestProfileAction_UnknownVerbUsageError verifies that an unknown sub-verb
// returns exit code 2 and does not silently succeed.
func TestProfileAction_UnknownVerbUsageError(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"bogus"}, home, "2026-05-28")
	if code != 2 {
		t.Errorf("profileAction(bogus): got exit code %d, want 2", code)
	}
}

// TestProfileAction_RefreshWritesFile verifies that "refresh" (no flags) creates
// profile.md and stamps the lastcheck attribute with the injected date.
// WHY: proves the main.go dispatch actually reaches Refresh; the profile-package
// unit tests cover the core logic, but this test verifies the wiring.
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

// TestProfileAction_IfStaleBadDuration verifies that --if-stale with an invalid
// duration returns exit code 1 (runtime error, not usage error). WHY: the spec
// requires an explicit parse error with non-zero exit; exit 2 is for usage errors.
func TestProfileAction_IfStaleBadDuration(t *testing.T) {
	home := t.TempDir()
	code := profileAction([]string{"refresh", "--if-stale", "7h"}, home, "2026-05-28")
	if code != 1 {
		t.Errorf("profileAction(refresh --if-stale 7h): got exit code %d, want 1", code)
	}
}

// TestProfileAction_IfStaleNoOpWhenFresh verifies that --if-stale with a fresh
// lastcheck does not modify the file. WHY: the --if-stale gate exists precisely
// to avoid spurious re-runs during session start.
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
