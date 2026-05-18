package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestIsRepoDev_withMarkerFile: a dir tree containing
// atomic/internal/bundlemirror/mirror.go relative to git toplevel is detected
// as repo-dev.
func TestIsRepoDev_withMarkerFile(t *testing.T) {
	root := t.TempDir()

	// Create the marker file the heuristic looks for.
	markerDir := filepath.Join(root, "atomic", "internal", "bundlemirror")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "mirror.go"), []byte("package bundlemirror"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Point cwd at the root (not a real git repo, but IsRepoDev falls back to
	// using cwd itself when git toplevel is unavailable).
	got, err := doctor.IsRepoDev(root)
	if err != nil {
		t.Fatalf("IsRepoDev: %v", err)
	}
	if !got {
		t.Error("IsRepoDev = false, want true when marker file present")
	}
}

// TestIsRepoDev_withoutMarkerFile: when marker is absent, returns false.
func TestIsRepoDev_withoutMarkerFile(t *testing.T) {
	root := t.TempDir()

	got, err := doctor.IsRepoDev(root)
	if err != nil {
		t.Fatalf("IsRepoDev: %v", err)
	}
	if got {
		t.Error("IsRepoDev = true, want false when marker file absent")
	}
}

// TestIsRepoDev_notInGitRepo: a cwd that is not a git repo is treated as not
// repo-dev (no git toplevel means no atomic-claude repo structure).
func TestIsRepoDev_notInGitRepo(t *testing.T) {
	// Use the t.TempDir() which is guaranteed to be outside any git repo tracked
	// by this test suite, and add no marker file.
	root := t.TempDir()

	got, err := doctor.IsRepoDev(root)
	if err != nil {
		t.Fatalf("IsRepoDev: %v", err)
	}
	if got {
		t.Error("IsRepoDev = true, want false for non-git-repo directory without marker")
	}
}
