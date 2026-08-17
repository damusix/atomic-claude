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
	// Path is relative to the target dir (e.g. "agents/atomic-builder.md").
	Path string `json:"path"`
	// SHA256 of the contents before install; empty when Existed is false.
	SHA256 string `json:"sha256"`
	// Existed is whether the file was on disk before install ran.
	Existed bool `json:"existed"`
}

// PreInstallManifest records what was on disk before the first atomic install,
// so `atomic claude uninstall` can restore that state. Written exactly once.
type PreInstallManifest struct {
	// Created is when the snapshot was taken.
	Created time.Time `json:"created"`
	// AtomicVersion is the binary that created the snapshot.
	AtomicVersion string `json:"atomic_version"`
	// Files holds one entry per artifact the install will touch, plus settings.json.
	Files []PreInstallFile `json:"files"`
}

// writePreInstallSnapshot copies every file the manifest will touch into
// <home>/.atomic/pre-install/. Write-once: a no-op if the directory exists.
func writePreInstallSnapshot(targetDir, home string, manifest []embedded.Artifact, clock Clock) error {
	preInstallDir := config.PreInstallDir(home)

	if _, err := os.Stat(preInstallDir); err == nil {
		return nil
	}

	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		return fmt.Errorf("mkdir pre-install: %w", err)
	}

	var files []PreInstallFile

	for _, a := range manifest {
		entry, err := snapshotFile(targetDir, preInstallDir, a.Target)
		if err != nil {
			return err
		}
		files = append(files, entry)
	}

	// settings.json is not an embedded artifact but install touches it too.
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

// snapshotFile copies relPath from targetDir into destDir, preserving subdirs.
// A missing file yields Existed=false and no copy.
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
