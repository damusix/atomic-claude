package claudeinstall_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// snapshotFixedClock returns a distinct fixed time for snapshot tests.
func snapshotFixedClock() time.Time {
	return time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
}

// TestSnapshotCreatedOnFirstInstall: first-time install creates pre-install dir and manifest.json.
func TestSnapshotCreatedOnFirstInstall(t *testing.T) {
	target := t.TempDir()

	_, err := claudeinstall.Install(target, false, snapshotFixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	preInstallDir := config.PreInstallDir(target)
	manifestPath := filepath.Join(preInstallDir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json not created: %v", err)
	}

	var m claudeinstall.PreInstallManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest.json invalid JSON: %v", err)
	}

	if m.Created.IsZero() {
		t.Error("manifest.json created timestamp is zero")
	}

	if m.AtomicVersion == "" {
		t.Error("manifest.json AtomicVersion is empty, want non-empty version string")
	}

	if len(m.Files) == 0 {
		t.Error("manifest.json has no file entries")
	}

	// Manifest must cover every artifact in the embedded manifest.
	embeddedTargets := make(map[string]bool)
	for _, a := range embedded.Manifest() {
		embeddedTargets[a.Target] = true
	}

	manifestTargets := make(map[string]bool)
	for _, f := range m.Files {
		manifestTargets[f.Path] = true
	}

	for target := range embeddedTargets {
		if !manifestTargets[target] {
			t.Errorf("embedded artifact %q missing from snapshot manifest", target)
		}
	}
}

// TestSnapshotNotOverwrittenOnSecondInstall: second install does not touch existing pre-install dir.
func TestSnapshotNotOverwrittenOnSecondInstall(t *testing.T) {
	target := t.TempDir()

	// First install.
	_, err := claudeinstall.Install(target, false, snapshotFixedClock)
	if err != nil {
		t.Fatalf("first Install: %v", err)
	}

	preInstallDir := config.PreInstallDir(target)
	manifestPath := filepath.Join(preInstallDir, "manifest.json")

	firstData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json after first install: %v", err)
	}

	// Second install with a different clock.
	laterClock := func() time.Time { return time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC) }
	_, err = claudeinstall.Install(target, false, laterClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	secondData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json after second install: %v", err)
	}

	if string(firstData) != string(secondData) {
		t.Error("pre-install manifest.json was modified on second install; want write-once semantics")
	}
}

// TestSnapshotCapturesExistingSettingsJSON: settings.json present before install is snapshotted.
func TestSnapshotCapturesExistingSettingsJSON(t *testing.T) {
	target := t.TempDir()

	settingsContent := []byte(`{"theme":"dark","fontSize":14}`)
	settingsPath := filepath.Join(target, "settings.json")
	if err := os.WriteFile(settingsPath, settingsContent, 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	_, err := claudeinstall.Install(target, false, snapshotFixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	preInstallDir := config.PreInstallDir(target)
	snappedSettings := filepath.Join(preInstallDir, "settings.json")

	data, err := os.ReadFile(snappedSettings)
	if err != nil {
		t.Fatalf("pre-install/settings.json not found: %v", err)
	}

	if string(data) != string(settingsContent) {
		t.Errorf("pre-install/settings.json content mismatch: got %q, want %q", data, settingsContent)
	}

	// manifest.json must record it as existed=true with correct sha256.
	manifestPath := filepath.Join(preInstallDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var m claudeinstall.PreInstallManifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	var settingsEntry *claudeinstall.PreInstallFile
	for i := range m.Files {
		if m.Files[i].Path == "settings.json" {
			settingsEntry = &m.Files[i]
			break
		}
	}

	if settingsEntry == nil {
		t.Fatal("settings.json not in manifest")
	}
	if !settingsEntry.Existed {
		t.Error("settings.json entry: existed=false, want true")
	}
	if settingsEntry.SHA256 == "" {
		t.Error("settings.json entry: SHA256 empty, want non-empty")
	}
}

// TestSnapshotRecordsMissingFilesAsNotExisted: files not on disk are recorded as existed=false.
func TestSnapshotRecordsMissingFilesAsNotExisted(t *testing.T) {
	target := t.TempDir()

	// Fresh target — nothing pre-exists.
	_, err := claudeinstall.Install(target, false, snapshotFixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	preInstallDir := config.PreInstallDir(target)
	manifestPath := filepath.Join(preInstallDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	var m claudeinstall.PreInstallManifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	// All files were absent before install (fresh target): each must have existed=false.
	for _, f := range m.Files {
		if f.Existed {
			t.Errorf("file %q: existed=true, want false (fresh install, nothing pre-existed)", f.Path)
		}
	}
}
