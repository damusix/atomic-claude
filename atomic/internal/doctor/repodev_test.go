package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func TestIsRepoDev_withMarkerFile(t *testing.T) {
	root := t.TempDir()

	markerDir := filepath.Join(root, "atomic", "internal", "bundlemirror")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "mirror.go"), []byte("package bundlemirror"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Not a real git repo; IsRepoDev falls back to cwd when there is no toplevel.
	got, err := doctor.IsRepoDev(root)
	if err != nil {
		t.Fatalf("IsRepoDev: %v", err)
	}
	if !got {
		t.Error("IsRepoDev = false, want true when marker file present")
	}
}

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

func TestIsRepoDev_notInGitRepo(t *testing.T) {
	// t.TempDir() is outside any git repo, and carries no marker.
	root := t.TempDir()

	got, err := doctor.IsRepoDev(root)
	if err != nil {
		t.Fatalf("IsRepoDev: %v", err)
	}
	if got {
		t.Error("IsRepoDev = true, want false for non-git-repo directory without marker")
	}
}
