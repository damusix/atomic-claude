// Package claudeinstall writes the embedded artifact bundle to a target directory
// (default ~/.claude) and manages backups for changed files.
package claudeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// Clock allows injecting a fixed time in tests.
type Clock func() time.Time

// RealClock returns time.Now().UTC().
func RealClock() time.Time { return time.Now().UTC() }

// ActionKind classifies what install/update will do to a file.
type ActionKind string

const (
	ActionInstalled     ActionKind = "installed"
	ActionUpdated       ActionKind = "updated"
	ActionUnchanged     ActionKind = "unchanged"
	ActionMergeRequired ActionKind = "merge_required"
)

// FileAction describes the planned or executed action for one artifact.
type FileAction struct {
	Artifact     embedded.Artifact
	Kind         ActionKind
	BackupPath   string // set when ActionUpdate
	ProposedPath string // set when ActionMergeRequired
}

// Plan computes the per-file action list without writing anything.
func Plan(targetDir string, manifest []embedded.Artifact) ([]FileAction, error) {
	var plan []FileAction
	for _, a := range manifest {
		fa, err := planArtifact(targetDir, a)
		if err != nil {
			return nil, err
		}
		plan = append(plan, fa)
	}
	return plan, nil
}

func planArtifact(targetDir string, a embedded.Artifact) (FileAction, error) {
	onDiskPath := filepath.Join(targetDir, filepath.FromSlash(a.Target))

	embeddedData, err := fs.ReadFile(embedded.FS, a.Source)
	if err != nil {
		return FileAction{}, fmt.Errorf("read embedded %s: %w", a.Source, err)
	}

	embeddedSHA := hexSHA256(embeddedData)

	diskData, err := os.ReadFile(onDiskPath)
	if os.IsNotExist(err) {
		return FileAction{Artifact: a, Kind: ActionInstalled}, nil
	}
	if err != nil {
		return FileAction{}, fmt.Errorf("read on-disk %s: %w", onDiskPath, err)
	}

	diskSHA := hexSHA256(diskData)
	if diskSHA == embeddedSHA {
		return FileAction{Artifact: a, Kind: ActionUnchanged}, nil
	}

	// Differs. CLAUDE.md gets the proposed-file treatment.
	if a.Target == "CLAUDE.md" {
		proposedPath := config.ProposedCLAUDEMD(targetDir)
		return FileAction{Artifact: a, Kind: ActionMergeRequired, ProposedPath: proposedPath}, nil
	}

	// Bundle-managed artifact: back up + overwrite.
	return FileAction{Artifact: a, Kind: ActionUpdated}, nil
}

// Apply executes a plan. If dryRun is true, no filesystem writes occur.
// clock is used for the backup timestamp — pass RealClock for production use.
func Apply(targetDir string, plan []FileAction, dryRun bool, clock Clock) error {
	// Capture the run-start time once so all backups in this run share the same
	// timestamp directory, regardless of when the first ActionUpdated is encountered.
	runStart := clock()

	// Compute the backup timestamp only when there are updates to make.
	var backupTimestamp string
	for _, fa := range plan {
		if fa.Kind == ActionUpdated {
			backupTimestamp = formatTimestamp(runStart)
			break
		}
	}

	for i := range plan {
		if err := applyAction(targetDir, &plan[i], dryRun, backupTimestamp); err != nil {
			return err
		}
	}
	if !dryRun {
		if err := ensureResolvedConfigStub(targetDir); err != nil {
			return err
		}
	}
	return nil
}

// ensureResolvedConfigStub creates <targetDir>/.atomic/config.resolved.md as an empty
// file if it does not already exist. Idempotent: leaves any existing content untouched.
// This file is @-referenced from CLAUDE.md so every Claude session can load it.
func ensureResolvedConfigStub(targetDir string) error {
	resolvedPath := config.ResolvedPath(targetDir)
	if _, err := os.Stat(resolvedPath); err == nil {
		return nil // already exists — leave it alone
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o755); err != nil {
		return fmt.Errorf("mkdir for config.resolved.md: %w", err)
	}
	return os.WriteFile(resolvedPath, []byte{}, 0o644)
}

func applyAction(targetDir string, fa *FileAction, dryRun bool, backupTimestamp string) error {
	onDiskPath := filepath.Join(targetDir, filepath.FromSlash(fa.Artifact.Target))

	embeddedData, err := fs.ReadFile(embedded.FS, fa.Artifact.Source)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", fa.Artifact.Source, err)
	}

	switch fa.Kind {
	case ActionInstalled:
		if dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(onDiskPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", onDiskPath, err)
		}
		return os.WriteFile(onDiskPath, embeddedData, 0o644)

	case ActionUpdated:
		backupPath := filepath.Join(config.BackupDir(targetDir), backupTimestamp, filepath.FromSlash(fa.Artifact.Target))
		fa.BackupPath = backupPath
		if dryRun {
			return nil
		}
		// Back up existing file.
		existing, err := os.ReadFile(onDiskPath)
		if err != nil {
			return fmt.Errorf("read existing for backup %s: %w", onDiskPath, err)
		}
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return fmt.Errorf("mkdir backup: %w", err)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		return os.WriteFile(onDiskPath, embeddedData, 0o644)

	case ActionMergeRequired:
		proposedPath := fa.ProposedPath
		if dryRun {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(proposedPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for proposed: %w", err)
		}
		return os.WriteFile(proposedPath, embeddedData, 0o644)

	case ActionUnchanged:
		// Nothing to do.
		return nil
	}
	return nil
}

// Install computes and applies the install plan. Equivalent to Update — same semantics.
func Install(targetDir string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installOrUpdate(targetDir, dryRun, clock)
}

// Update is the same flow as Install.
func Update(targetDir string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installOrUpdate(targetDir, dryRun, clock)
}

func installOrUpdate(targetDir string, dryRun bool, clock Clock) ([]FileAction, error) {
	manifest := embedded.Manifest()

	// Capture pre-install state before any files are written. Write-once: if the
	// snapshot dir already exists this is a no-op. Skip when dry-running.
	if !dryRun {
		if err := writePreInstallSnapshot(targetDir, manifest, clock); err != nil {
			return nil, fmt.Errorf("pre-install snapshot: %w", err)
		}
	}

	plan, err := Plan(targetDir, manifest)
	if err != nil {
		return nil, err
	}
	if err := Apply(targetDir, plan, dryRun, clock); err != nil {
		return nil, err
	}
	return plan, nil
}

// DiffStatus is the per-artifact comparison result for the diff verb.
type DiffStatus string

const (
	DiffMatch  DiffStatus = "match"
	DiffDiffer DiffStatus = "diff"
	DiffAbsent DiffStatus = "absent"
)

// DiffRow is one row in the diff output.
type DiffRow struct {
	Status   DiffStatus
	Artifact embedded.Artifact
}

// Diff compares each manifest artifact against the on-disk state. Read-only.
func Diff(targetDir string) ([]DiffRow, error) {
	manifest := embedded.Manifest()
	var rows []DiffRow
	for _, a := range manifest {
		onDiskPath := filepath.Join(targetDir, filepath.FromSlash(a.Target))

		embeddedData, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			return nil, fmt.Errorf("read embedded %s: %w", a.Source, err)
		}

		diskData, err := os.ReadFile(onDiskPath)
		if os.IsNotExist(err) {
			rows = append(rows, DiffRow{Status: DiffAbsent, Artifact: a})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read on-disk %s: %w", onDiskPath, err)
		}

		if hexSHA256(embeddedData) == hexSHA256(diskData) {
			rows = append(rows, DiffRow{Status: DiffMatch, Artifact: a})
		} else {
			rows = append(rows, DiffRow{Status: DiffDiffer, Artifact: a})
		}
	}
	return rows, nil
}

// ListRow is one row in the list output.
type ListRow struct {
	Kind   string
	Target string
	SHA256 string
}

// List returns all manifest artifacts in stable sort order (kind asc, target asc).
func List() []ListRow {
	manifest := embedded.Manifest()
	rows := make([]ListRow, len(manifest))
	for i, a := range manifest {
		rows[i] = ListRow{Kind: a.Kind, Target: a.Target, SHA256: a.SHA256}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Target < rows[j].Target
	})
	return rows
}

// Report renders the final summary for install/update.
func Report(plan []FileAction, targetDir string) string {
	var installed, updated, unchanged, mergeRequired []FileAction

	for _, fa := range plan {
		switch fa.Kind {
		case ActionInstalled:
			installed = append(installed, fa)
		case ActionUpdated:
			updated = append(updated, fa)
		case ActionUnchanged:
			unchanged = append(unchanged, fa)
		case ActionMergeRequired:
			mergeRequired = append(mergeRequired, fa)
		}
	}

	// Compute the shared backup directory from the first updated action's BackupPath.
	// BackupPath shape: <targetDir>/.atomic/backups/<timestamp>/<relpath>
	// We want: <targetDir>/.atomic/backups/<timestamp>
	backupDir := ""
	for _, fa := range updated {
		if fa.BackupPath != "" {
			// The relpath inside BackupPath matches fa.Artifact.Target, so strip it off.
			rel := filepath.FromSlash(fa.Artifact.Target)
			candidate := strings.TrimSuffix(fa.BackupPath, string(os.PathSeparator)+rel)
			if candidate != fa.BackupPath {
				backupDir = candidate
			} else {
				backupDir = filepath.Dir(fa.BackupPath)
			}
			break
		}
	}

	var sb strings.Builder
	sb.WriteString("Atomic Claude install summary\n")

	if len(installed) > 0 {
		fmt.Fprintf(&sb, "\nInstalled (%d):\n", len(installed))
		for _, fa := range installed {
			fmt.Fprintf(&sb, "  ✓ %s\n", fa.Artifact.Target)
		}
	}

	if len(updated) > 0 {
		fmt.Fprintf(&sb, "\nUpdated (%d, backed up to %s/):\n", len(updated), backupDir)
		for _, fa := range updated {
			fmt.Fprintf(&sb, "  ↻ %s\n", fa.Artifact.Target)
		}
	}

	if len(unchanged) > 0 {
		fmt.Fprintf(&sb, "\nUnchanged (%d):\n", len(unchanged))
		for _, fa := range unchanged {
			fmt.Fprintf(&sb, "  • %s\n", fa.Artifact.Target)
		}
	}

	if len(mergeRequired) > 0 {
		fmt.Fprintf(&sb, "\nNeeds review (%d):\n", len(mergeRequired))
		for _, fa := range mergeRequired {
			absTarget := filepath.Join(targetDir, fa.Artifact.Target)
			fmt.Fprintf(&sb, "  ⚠ %s\n", absTarget)
			fmt.Fprintf(&sb, "    proposed at %s\n", fa.ProposedPath)
			fmt.Fprintf(&sb, "    next step: run /atomic-claude-merge inside any Claude Code session\n")
		}
	}

	return sb.String()
}

// formatTimestamp formats a time as ISO-8601 with colons replaced by hyphens
// so the string is safe for use as a directory name on all platforms.
func formatTimestamp(t time.Time) string {
	s := t.UTC().Format(time.RFC3339)
	// Replace colons with hyphens: 2026-05-16T18:32:11Z → 2026-05-16T18-32-11Z
	return strings.ReplaceAll(s, ":", "-")
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ResolveTarget expands "~" in the target path.
func ResolveTarget(target string) (string, error) {
	if target == "" || target == "~/.claude" || target == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if target == "~" {
			return home, nil
		}
		return filepath.Join(home, ".claude"), nil
	}
	if strings.HasPrefix(target, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Join(home, target[2:]), nil
	}
	return target, nil
}
