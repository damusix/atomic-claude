package claudeinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

func fixedClock() time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
}

func TestWritePreInstallSnapshot_CustomManifest(t *testing.T) {
	targetDir := t.TempDir()

	existingRelPath := "agents/atomic-builder.md"
	existingContent := []byte("# atomic-builder agent")
	existingOnDisk := filepath.Join(targetDir, filepath.FromSlash(existingRelPath))
	if err := os.MkdirAll(filepath.Dir(existingOnDisk), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(existingOnDisk, existingContent, 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	artifacts := []embedded.Artifact{
		{Target: existingRelPath, SHA256: "irrelevant-for-snapshot"},
		{Target: "agents/atomic-surgeon.md", SHA256: "also-irrelevant"},
	}

	if err := writePreInstallSnapshot(targetDir, targetDir, artifacts, fixedClock); err != nil {
		t.Fatalf("writePreInstallSnapshot: %v", err)
	}

	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")
	manifestPath := filepath.Join(preInstallDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json not created: %v", err)
	}

	var m PreInstallManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	if m.Created.IsZero() {
		t.Error("Created timestamp is zero")
	}

	byPath := make(map[string]PreInstallFile, len(m.Files))
	for _, f := range m.Files {
		byPath[f.Path] = f
	}

	existing, ok := byPath[existingRelPath]
	if !ok {
		t.Fatalf("manifest missing entry for %q", existingRelPath)
	}
	if !existing.Existed {
		t.Errorf("%q: existed=false, want true", existingRelPath)
	}
	if existing.SHA256 == "" {
		t.Errorf("%q: SHA256 empty, want non-empty", existingRelPath)
	}
	snappedCopy := filepath.Join(preInstallDir, filepath.FromSlash(existingRelPath))
	if _, err := os.Stat(snappedCopy); err != nil {
		t.Errorf("pre-install copy not created at %s: %v", snappedCopy, err)
	}

	absentRelPath := "agents/atomic-surgeon.md"
	absent, ok := byPath[absentRelPath]
	if !ok {
		t.Fatalf("manifest missing entry for %q", absentRelPath)
	}
	if absent.Existed {
		t.Errorf("%q: existed=true, want false", absentRelPath)
	}
	if absent.SHA256 != "" {
		t.Errorf("%q: SHA256=%q, want empty", absentRelPath, absent.SHA256)
	}
}

func TestWritePreInstallSnapshot_WriteOnce(t *testing.T) {
	targetDir := t.TempDir()

	artifacts := []embedded.Artifact{
		{Target: "CLAUDE.md", SHA256: "abc"},
	}

	clock1 := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	if err := writePreInstallSnapshot(targetDir, targetDir, artifacts, clock1); err != nil {
		t.Fatalf("first writePreInstallSnapshot: %v", err)
	}

	clock2 := func() time.Time { return time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC) }
	if err := writePreInstallSnapshot(targetDir, targetDir, artifacts, clock2); err != nil {
		t.Fatalf("second writePreInstallSnapshot: %v", err)
	}

	manifestPath := filepath.Join(targetDir, ".atomic", "pre-install", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var m PreInstallManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !m.Created.Equal(want) {
		t.Errorf("Created = %v, want %v (write-once: second call must not overwrite)", m.Created, want)
	}
}
