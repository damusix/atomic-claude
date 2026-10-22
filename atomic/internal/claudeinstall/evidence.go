package claudeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// This file exposes read-only views of the legacy Claude installer's own
// output, for the migration classifier. It writes nothing: classification must
// be answerable before any adoption plan is approved.

// InstallRecord is the legacy `[install]` section of ~/.atomic/config.toml: the
// version string the last installer recorded and the target-relative artifact
// paths it copied. An empty record means the section is absent, which is valid
// for a pre-framework install.
type InstallRecord struct {
	Version string
	Targets []string
}

// Present reports whether the [install] section carries any evidence.
func (r InstallRecord) Present() bool {
	return r.Version != "" || len(r.Targets) > 0
}

// ReadInstallRecord reads the legacy [install] section. A missing config file is
// an empty record, not an error.
func ReadInstallRecord(home string) (InstallRecord, error) {
	cfg, _, err := config.Load(config.TOMLPath(home))
	if err != nil {
		return InstallRecord{}, err
	}
	rec := InstallRecord{Version: cfg.Install.Version}
	for _, group := range [][]string{
		cfg.Install.Artifacts.Agents,
		cfg.Install.Artifacts.Commands,
		cfg.Install.Artifacts.Skills,
		cfg.Install.Artifacts.OutputStyles,
		cfg.Install.Artifacts.Rules,
	} {
		rec.Targets = append(rec.Targets, group...)
	}
	return rec, nil
}

// SnapshotState is the structural health of the write-once pre-install snapshot.
type SnapshotState string

const (
	// SnapshotMissing means no snapshot directory was ever written: there is no
	// historical restoration evidence, and adoption must acknowledge that limit.
	SnapshotMissing SnapshotState = "missing"
	// SnapshotValid means the manifest parses and every recorded copy is present
	// and digest-matches what the manifest recorded.
	SnapshotValid SnapshotState = "valid"
	// SnapshotCorrupt means the manifest is unreadable or a recorded copy is
	// missing or tampered with. The snapshot is preserved, never rewritten, and
	// automatic adoption is blocked.
	SnapshotCorrupt SnapshotState = "corrupt"
)

// SnapshotReport is the read-only state of the legacy pre-install snapshot.
type SnapshotReport struct {
	State SnapshotState
	// Files counts the manifest's recorded entries.
	Files int
	// MissingCopies names recorded entries whose stored copy is absent or does
	// not match the manifest digest.
	MissingCopies []string
	// Detail explains a corrupt or missing snapshot.
	Detail string
}

// ReadSnapshot inspects ~/.atomic/pre-install without modifying it. The
// manifest's own recorded digests are the verification basis; a copy that no
// longer matches is corruption, not ownership.
func ReadSnapshot(home string) SnapshotReport {
	dir := config.PreInstallDir(home)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return SnapshotReport{State: SnapshotMissing, Detail: "no pre-install snapshot directory"}
		}
		return SnapshotReport{State: SnapshotCorrupt, Detail: fmt.Sprintf("stat %s: %v", dir, err)}
	}

	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return SnapshotReport{State: SnapshotCorrupt, Detail: fmt.Sprintf("read %s: %v", manifestPath, err)}
	}

	var m PreInstallManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return SnapshotReport{State: SnapshotCorrupt, Detail: fmt.Sprintf("parse %s: %v", manifestPath, err)}
	}

	report := SnapshotReport{State: SnapshotValid, Files: len(m.Files)}
	for _, f := range m.Files {
		if !f.Existed {
			continue
		}
		copied, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			report.MissingCopies = append(report.MissingCopies, f.Path)
			continue
		}
		sum := sha256.Sum256(copied)
		if f.SHA256 != "" && hex.EncodeToString(sum[:]) != f.SHA256 {
			report.MissingCopies = append(report.MissingCopies, f.Path)
		}
	}
	if len(report.MissingCopies) > 0 {
		report.State = SnapshotCorrupt
		report.Detail = "recorded copies are missing or do not match the manifest"
	}
	return report
}

// ProposedPath is where the legacy installer wrote a diverged CLAUDE.md for the
// user to review, and MergedPath is where the merge agent writes the candidate
// merge. Both are adoption blockers while they exist.
func ProposedPath(home string) string { return config.ProposedCLAUDEMD(home) }

// MergedPath returns <nativeRoot>/CLAUDE.md.atomic-merged.
func MergedPath(nativeRoot string) string {
	return filepath.Join(nativeRoot, "CLAUDE.md.atomic-merged")
}
