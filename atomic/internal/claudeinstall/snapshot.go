package claudeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// PreInstallFile records one file's pre-install state in the snapshot manifest.
type PreInstallFile struct {
	// Path is the relative path within the target dir (e.g. "agents/atomic-builder.md").
	Path string `json:"path"`
	// SHA256 is the hex-encoded SHA256 of the file's contents before install.
	// Empty when Existed is false.
	SHA256 string `json:"sha256"`
	// Existed is true if the file was present on disk before install ran.
	Existed bool `json:"existed"`
}

// PreInstallManifest is written to <targetDir>/.atomic/pre-install/manifest.json
// exactly once (write-once semantics). It records what was on disk before the
// first atomic install so that `atomic claude uninstall` can restore the state.
type PreInstallManifest struct {
	// Created is the UTC timestamp when the snapshot was taken.
	Created time.Time `json:"created"`
	// AtomicVersion is the version string of the binary that created the snapshot.
	AtomicVersion string `json:"atomic_version"`
	// Files contains one entry per artifact the install will touch, plus settings.json.
	Files []PreInstallFile `json:"files"`
}

// writePreInstallSnapshot captures a snapshot of all files the manifest will
// touch into <targetDir>/.atomic/pre-install/. Called once, before Apply().
// If pre-install/ already exists, this is a no-op (write-once semantics).
func writePreInstallSnapshot(targetDir string, manifest []embedded.Artifact, clock Clock) error {
	preInstallDir := config.PreInstallDir(targetDir)

	// Guard: if the directory already exists, skip. Write-once.
	if _, err := os.Stat(preInstallDir); err == nil {
		return nil
	}

	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		return fmt.Errorf("mkdir pre-install: %w", err)
	}

	var files []PreInstallFile

	// Snapshot every embedded artifact target.
	for _, a := range manifest {
		entry, err := snapshotFile(targetDir, preInstallDir, a.Target)
		if err != nil {
			return err
		}
		files = append(files, entry)
	}

	// Also snapshot settings.json — not an embedded artifact but always relevant.
	settingsEntry, err := snapshotFile(targetDir, preInstallDir, "settings.json")
	if err != nil {
		return err
	}
	files = append(files, settingsEntry)

	m := PreInstallManifest{
		Created:       clock().UTC(),
		AtomicVersion: version.Version,
		Files:         files,
	}

	manifestData, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal pre-install manifest: %w", err)
	}

	manifestPath := filepath.Join(preInstallDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return fmt.Errorf("write pre-install manifest: %w", err)
	}

	return nil
}

// snapshotFile copies srcRel from targetDir into destDir (preserving subdirs)
// and returns a PreInstallFile entry. If the file doesn't exist, the entry has
// Existed=false and no copy is made.
func snapshotFile(targetDir, destDir, relPath string) (PreInstallFile, error) {
	src := filepath.Join(targetDir, filepath.FromSlash(relPath))
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return PreInstallFile{Path: relPath, SHA256: "", Existed: false}, nil
	}
	if err != nil {
		return PreInstallFile{}, fmt.Errorf("read pre-install source %s: %w", relPath, err)
	}

	dest := filepath.Join(destDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return PreInstallFile{}, fmt.Errorf("mkdir for pre-install %s: %w", relPath, err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return PreInstallFile{}, fmt.Errorf("write pre-install %s: %w", relPath, err)
	}

	sum := sha256.Sum256(data)
	return PreInstallFile{
		Path:    relPath,
		SHA256:  hex.EncodeToString(sum[:]),
		Existed: true,
	}, nil
}
