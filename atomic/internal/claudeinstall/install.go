// Package claudeinstall writes the embedded artifact bundle to a target directory
// (default ~/.claude) and manages backups for changed files.
package claudeinstall

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

// Clock allows injecting a fixed time in tests.
type Clock func() time.Time

// RealClock returns time.Now().UTC().
func RealClock() time.Time { return time.Now().UTC() }

// DefaultProfileRefresh is the real implementation used in production.
// Exposed so tests can restore the original after overriding ProfileRefresh.
var DefaultProfileRefresh = profile.RefreshIfStale

// ProfileRefresh is an injectable seam for tests: swap it with a spy to capture
// calls without real detection, home-dir resolution, or disk writes. Production
// code always calls DefaultProfileRefresh; only tests override this.
var ProfileRefresh = profile.RefreshIfStale

// ActionKind classifies what install/update will do to a file.
type ActionKind string

const (
	ActionInstalled     ActionKind = "installed"
	ActionUpdated       ActionKind = "updated"
	ActionUnchanged     ActionKind = "unchanged"
	ActionMergeRequired ActionKind = "merge_required"
	// ActionBlockReplaced: CLAUDE.md only — the on-disk file carries an
	// <atomic> block that differs from the embedded one. The block is
	// replaced in place; user content outside it is preserved.
	ActionBlockReplaced ActionKind = "block_replaced"
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

	// Differs. CLAUDE.md is block-aware: the <atomic>...</atomic> block is
	// atomic-owned, everything outside is user-owned. When both sides carry a
	// parseable block, compare and replace only the block — user content
	// outside it never causes drift or a merge. The proposed-file + LLM merge
	// path remains only for files without a parseable block (pre-tag installs,
	// malformed tags) where code cannot draw the ownership boundary safely.
	if a.Target == "CLAUDE.md" {
		embBlock, embOK := extractAtomicBlock(string(embeddedData))
		diskBlock, diskOK := extractAtomicBlock(string(diskData))
		if embOK && diskOK {
			if embBlock == diskBlock {
				return FileAction{Artifact: a, Kind: ActionUnchanged}, nil
			}
			return FileAction{Artifact: a, Kind: ActionBlockReplaced}, nil
		}
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
		if fa.Kind == ActionUpdated || fa.Kind == ActionBlockReplaced {
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

// ProfileNudge is the bootstrap message printed to stdout when profile.md is
// created for the first time. Tests reference this constant to avoid duplicating
// the verbatim string.
const ProfileNudge = "Profile created at ~/.claude/.atomic/profile.md. Mention your role, projects, and preferences in conversation and Claude will record them. Run /atomic-improve to review drift."

// ensureProfileStub creates <targetDir>/.atomic/profile.md with the initial schema
// template if it does not already exist. Idempotent: leaves any existing content untouched.
// When the file is created, it prints a bootstrap nudge to out.
// Returns (true, nil) when the file was created, (false, nil) when it already existed.
func ensureProfileStub(targetDir string, out io.Writer) (bool, error) {
	profilePath := config.ProfilePath(targetDir)
	if _, err := os.Stat(profilePath); err == nil {
		return false, nil // already exists — leave it alone
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir for profile.md: %w", err)
	}
	e := profile.CaptureEnv()
	content := profile.RenderStub(e)
	if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write profile.md: %w", err)
	}
	fmt.Fprintln(out, ProfileNudge)
	return true, nil
}

// populateProfile calls the profileRefresh seam in a best-effort manner.
// Any error returned by the seam and any panic are silently swallowed so
// install/update never fails due to a detection error.
// today is derived from clock so tests can inject a fixed date.
func populateProfile(targetDir string, clock Clock) {
	defer func() { recover() }() // best-effort: swallows any panic from detection so install never fails
	today := clock().Format("2006-01-02")
	_, _ = ProfileRefresh(targetDir, today, profile.DefaultRefreshDays)
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

	case ActionBlockReplaced:
		backupPath := filepath.Join(config.BackupDir(targetDir), backupTimestamp, filepath.FromSlash(fa.Artifact.Target))
		fa.BackupPath = backupPath
		if dryRun {
			return nil
		}
		existing, err := os.ReadFile(onDiskPath)
		if err != nil {
			return fmt.Errorf("read existing for backup %s: %w", onDiskPath, err)
		}
		embBlock, ok := extractAtomicBlock(string(embeddedData))
		if !ok {
			return fmt.Errorf("embedded %s has no parseable <atomic> block", fa.Artifact.Target)
		}
		merged, ok := replaceAtomicBlock(string(existing), embBlock)
		if !ok {
			// Plan saw a parseable block; the file changed between plan and
			// apply. Fail loud rather than guessing the boundary.
			return fmt.Errorf("%s lost its parseable <atomic> block between plan and apply", onDiskPath)
		}
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return fmt.Errorf("mkdir backup: %w", err)
		}
		if err := os.WriteFile(backupPath, existing, 0o644); err != nil {
			return fmt.Errorf("write backup %s: %w", backupPath, err)
		}
		return os.WriteFile(onDiskPath, []byte(merged), 0o644)

	case ActionUnchanged:
		// Nothing to do.
		return nil
	}
	return nil
}

// Install computes and applies the install plan. Equivalent to Update — same semantics.
// Profile nudge goes to os.Stdout.
func Install(targetDir string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installWithOutput(targetDir, dryRun, clock, os.Stdout)
}

// installWithOutput is Install with a configurable writer for the profile bootstrap nudge.
// Unexported — exported via export_test.go for test use only.
func installWithOutput(targetDir string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
	return installOrUpdate(targetDir, dryRun, clock, out)
}

// Update is the same flow as Install.
func Update(targetDir string, dryRun bool, clock Clock) ([]FileAction, error) {
	return installOrUpdate(targetDir, dryRun, clock, os.Stdout)
}

func installOrUpdate(targetDir string, dryRun bool, clock Clock, out io.Writer) ([]FileAction, error) {
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
	if !dryRun {
		// ensureProfileStub is intentionally called here (install/update level),
		// NOT inside Apply. Apply handles bundle artifacts; profile.md is user-data
		// that should never be overwritten by a plain Apply call (e.g. a dry-run
		// caller or a future Apply-only code path). Keeping it here ensures the file
		// is only created when a real install/update is requested.
		if _, err := ensureProfileStub(targetDir, out); err != nil {
			return nil, err
		}
		// After the stub exists, populate the ## Environment fingerprint.
		// Called unconditionally: RefreshIfStale self-gates on lastcheck.
		// Fresh install: stub has no lastcheck → stale → full detect.
		// Re-install/update with fresh lastcheck: no-op.
		// Best-effort: any error or panic is swallowed — install must not fail
		// because detection failed (mirrors the session-start hook's swallow behavior).
		populateProfile(targetDir, clock)
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

		switch {
		case hexSHA256(embeddedData) == hexSHA256(diskData):
			rows = append(rows, DiffRow{Status: DiffMatch, Artifact: a})
		case a.Target == "CLAUDE.md" && atomicBlocksEqual(embeddedData, diskData):
			// Merged CLAUDE.md: user content outside the <atomic> block is
			// expected and is not drift. Only a stale block differs.
			rows = append(rows, DiffRow{Status: DiffMatch, Artifact: a})
		default:
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
	var installed, updated, unchanged, mergeRequired, blockReplaced []FileAction

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
		case ActionBlockReplaced:
			blockReplaced = append(blockReplaced, fa)
		}
	}

	// Compute the shared backup directory from the first updated action's BackupPath.
	// BackupPath shape: <targetDir>/.atomic/backups/<timestamp>/<relpath>
	// We want: <targetDir>/.atomic/backups/<timestamp>
	backupDir := ""
	for _, fa := range append(append([]FileAction{}, updated...), blockReplaced...) {
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

	if len(blockReplaced) > 0 {
		fmt.Fprintf(&sb, "\nUpdated <atomic> block (%d, backed up to %s/):\n", len(blockReplaced), backupDir)
		for _, fa := range blockReplaced {
			fmt.Fprintf(&sb, "  ↻ %s (your content outside <atomic> preserved)\n", fa.Artifact.Target)
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
