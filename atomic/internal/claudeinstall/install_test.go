package claudeinstall_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// fixedClock returns a deterministic timestamp for tests.
func fixedClock() time.Time {
	return time.Date(2026, 5, 16, 18, 32, 11, 0, time.UTC)
}

// --- helpers ---

func readEmbedded(t *testing.T, source string) []byte {
	t.Helper()
	data, err := fs.ReadFile(embedded.FS, source)
	if err != nil {
		t.Fatalf("read embedded %s: %v", source, err)
	}
	return data
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func countKind(plan []claudeinstall.FileAction, kind claudeinstall.ActionKind) int {
	n := 0
	for _, fa := range plan {
		if fa.Kind == kind {
			n++
		}
	}
	return n
}

// --- tests ---

// TestInstallIntoEmptyTarget: first-time install writes all artifacts.
func TestInstallIntoEmptyTarget(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	manifest := embedded.Manifest()
	if len(plan) != len(manifest) {
		t.Fatalf("plan len = %d, want %d", len(plan), len(manifest))
	}

	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed != len(manifest) {
		t.Errorf("installed count = %d, want %d (all)", installed, len(manifest))
	}

	// Verify each file exists on disk.
	for _, fa := range plan {
		onDisk := filepath.Join(target, filepath.FromSlash(fa.Artifact.Target))
		data, err := os.ReadFile(onDisk)
		if err != nil {
			t.Errorf("on-disk %s: %v", fa.Artifact.Target, err)
			continue
		}
		embeddedData := readEmbedded(t, fa.Artifact.Source)
		if sha256hex(data) != sha256hex(embeddedData) {
			t.Errorf("sha mismatch for %s", fa.Artifact.Target)
		}
	}

	// No proposed file for CLAUDE.md on fresh install.
	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Errorf(".atomic/proposed/CLAUDE.md should not exist on fresh install")
	}
}

// TestInstallSecondRunAllUnchanged: re-running after full install reports all unchanged.
func TestInstallSecondRunAllUnchanged(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	unchanged := countKind(plan, claudeinstall.ActionUnchanged)
	if unchanged != len(plan) {
		t.Errorf("second run: unchanged = %d, want %d", unchanged, len(plan))
	}
}

// TestInstallUpdatesChangedArtifact: hand-edited bundle artifact gets backed up and overwritten.
func TestInstallUpdatesChangedArtifact(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	// Hand-edit agents/atomic-builder.md.
	editPath := filepath.Join(target, "agents", "atomic-builder.md")
	original, _ := os.ReadFile(editPath)
	tampered := append(original, []byte("\ntampered\n")...)
	if err := os.WriteFile(editPath, tampered, 0o644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	updated := countKind(plan, claudeinstall.ActionUpdated)
	if updated != 1 {
		t.Errorf("updated = %d, want 1", updated)
	}

	// Find the updated action.
	var updatedAction *claudeinstall.FileAction
	for i := range plan {
		if plan[i].Kind == claudeinstall.ActionUpdated {
			updatedAction = &plan[i]
		}
	}
	if updatedAction == nil {
		t.Fatal("no updated action in plan")
	}

	// Backup file must exist and contain the tampered bytes.
	backupData, err := os.ReadFile(updatedAction.BackupPath)
	if err != nil {
		t.Fatalf("backup %s: %v", updatedAction.BackupPath, err)
	}
	if sha256hex(backupData) != sha256hex(tampered) {
		t.Errorf("backup content doesn't match tampered content")
	}

	// On-disk file must now match embedded bytes.
	onDisk, _ := os.ReadFile(editPath)
	embedded := readEmbedded(t, "bundle/agents/atomic-builder.md")
	if sha256hex(onDisk) != sha256hex(embedded) {
		t.Errorf("on-disk file not restored to embedded content")
	}
}

// TestInstallCLAUDEmdDiffers: existing CLAUDE.md that differs → proposed file; original untouched.
func TestInstallCLAUDEmdDiffers(t *testing.T) {
	target := t.TempDir()

	// Pre-install with hand-crafted CLAUDE.md.
	claudePath := filepath.Join(target, "CLAUDE.md")
	userContent := []byte("# My custom CLAUDE.md\n\nCustom content.\n")
	if err := os.WriteFile(claudePath, userContent, 0o644); err != nil {
		t.Fatalf("write user CLAUDE.md: %v", err)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	mergeRequired := countKind(plan, claudeinstall.ActionMergeRequired)
	if mergeRequired != 1 {
		t.Errorf("merge_required = %d, want 1", mergeRequired)
	}

	// Original CLAUDE.md must be untouched.
	current, _ := os.ReadFile(claudePath)
	if sha256hex(current) != sha256hex(userContent) {
		t.Errorf("CLAUDE.md was modified; should be untouched")
	}

	// Proposed file must exist at new path and contain embedded bytes.
	proposedPath := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	proposed, err := os.ReadFile(proposedPath)
	if err != nil {
		t.Fatalf("proposed file: %v", err)
	}
	embeddedClaude := readEmbedded(t, "bundle/CLAUDE.md")
	if sha256hex(proposed) != sha256hex(embeddedClaude) {
		t.Errorf("proposed file does not match embedded CLAUDE.md")
	}
}

// TestInstallCLAUDEmdIdentical: CLAUDE.md matching embedded → unchanged, no proposed.
func TestInstallCLAUDEmdIdentical(t *testing.T) {
	target := t.TempDir()

	// Write embedded CLAUDE.md to target first.
	embeddedClaude := readEmbedded(t, "bundle/CLAUDE.md")
	claudePath := filepath.Join(target, "CLAUDE.md")
	if err := os.WriteFile(claudePath, embeddedClaude, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// CLAUDE.md should be unchanged.
	for _, fa := range plan {
		if fa.Artifact.Target == "CLAUDE.md" {
			if fa.Kind != claudeinstall.ActionUnchanged {
				t.Errorf("CLAUDE.md action = %s, want unchanged", fa.Kind)
			}
		}
	}

	// No proposed file.
	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Errorf(".atomic/proposed/CLAUDE.md should not exist when CLAUDE.md is unchanged")
	}
}

// TestDryRunNoWrites: --dry-run makes no filesystem changes.
func TestDryRunNoWrites(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Install(target, true /* dryRun */, fixedClock)
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}

	// Should have planned installs.
	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed == 0 {
		t.Errorf("dry-run plan has zero installs — unexpected")
	}

	// But no files written.
	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote files: %v", entries)
	}
}

// TestListStableOrder: List returns all manifest rows sorted by kind then target.
func TestListStableOrder(t *testing.T) {
	rows := claudeinstall.List()
	if len(rows) == 0 {
		t.Fatal("List returned empty")
	}

	manifest := embedded.Manifest()
	if len(rows) != len(manifest) {
		t.Fatalf("List len = %d, want %d", len(rows), len(manifest))
	}

	for i := 1; i < len(rows); i++ {
		prev := rows[i-1]
		curr := rows[i]
		less := prev.Kind < curr.Kind || (prev.Kind == curr.Kind && prev.Target <= curr.Target)
		if !less {
			t.Errorf("rows not sorted at index %d: %q/%q vs %q/%q", i, prev.Kind, prev.Target, curr.Kind, curr.Target)
		}
	}
}

// TestListTabSeparated: Spot-check that List row fields are non-empty.
func TestListTabSeparated(t *testing.T) {
	rows := claudeinstall.List()
	for _, r := range rows {
		if r.Kind == "" || r.Target == "" || r.SHA256 == "" {
			t.Errorf("empty field in row: %+v", r)
		}
	}
}

// TestDiffAllAbsent: Diff against empty dir shows all absent.
func TestDiffAllAbsent(t *testing.T) {
	target := t.TempDir()

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, r := range rows {
		if r.Status != claudeinstall.DiffAbsent {
			t.Errorf("%s: status = %s, want absent", r.Artifact.Target, r.Status)
		}
	}
}

// TestDiffAllMatch: Diff after full install shows all match.
func TestDiffAllMatch(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, r := range rows {
		if r.Status != claudeinstall.DiffMatch {
			t.Errorf("%s: status = %s, want match", r.Artifact.Target, r.Status)
		}
	}
}

// TestDiffMixed: half-installed shows mixed status.
func TestDiffMixed(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Remove one file to make it absent.
	absentTarget := "agents/atomic-builder.md"
	if err := os.Remove(filepath.Join(target, absentTarget)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Tamper one file to make it diff.
	diffTarget := "agents/atomic-reviewer.md"
	diffPath := filepath.Join(target, diffTarget)
	existing, _ := os.ReadFile(diffPath)
	_ = os.WriteFile(diffPath, append(existing, []byte("\ntampered\n")...), 0o644)

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	statusFor := func(target string) claudeinstall.DiffStatus {
		for _, r := range rows {
			if r.Artifact.Target == target {
				return r.Status
			}
		}
		return ""
	}

	if statusFor(absentTarget) != claudeinstall.DiffAbsent {
		t.Errorf("%s: want absent, got %s", absentTarget, statusFor(absentTarget))
	}
	if statusFor(diffTarget) != claudeinstall.DiffDiffer {
		t.Errorf("%s: want diff, got %s", diffTarget, statusFor(diffTarget))
	}
	// Everything else should be match.
	for _, r := range rows {
		if r.Artifact.Target == absentTarget || r.Artifact.Target == diffTarget {
			continue
		}
		if r.Status != claudeinstall.DiffMatch {
			t.Errorf("%s: want match, got %s", r.Artifact.Target, r.Status)
		}
	}
}

// TestManifestSHAMatchesEmbedded: manifest SHA256 values match actual embedded bytes.
func TestManifestSHAMatchesEmbedded(t *testing.T) {
	for _, a := range embedded.Manifest() {
		data := readEmbedded(t, a.Source)
		actual := sha256hex(data)
		if actual != a.SHA256 {
			t.Errorf("%s: manifest SHA = %s, actual = %s", a.Source, a.SHA256, actual)
		}
	}
}

// TestUpdate_DelegatesToInstall: Update() installs the same artifact set as Install().
func TestUpdate_DelegatesToInstall(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Update(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	manifest := embedded.Manifest()
	if len(plan) != len(manifest) {
		t.Fatalf("Update plan len = %d, want %d", len(plan), len(manifest))
	}

	// All artifacts should be installed on a fresh target.
	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed != len(manifest) {
		t.Errorf("Update installed = %d, want %d (all)", installed, len(manifest))
	}

	// All files must exist on disk with the correct content.
	for _, fa := range plan {
		onDisk := filepath.Join(target, filepath.FromSlash(fa.Artifact.Target))
		data, err := os.ReadFile(onDisk)
		if err != nil {
			t.Errorf("Update: on-disk %s: %v", fa.Artifact.Target, err)
			continue
		}
		embeddedData := readEmbedded(t, fa.Artifact.Source)
		if sha256hex(data) != sha256hex(embeddedData) {
			t.Errorf("Update: sha mismatch for %s", fa.Artifact.Target)
		}
	}
}

// TestBackupPathContainsTimestamp: backup path includes the fixed timestamp.
func TestBackupPathContainsTimestamp(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	editPath := filepath.Join(target, "agents", "atomic-builder.md")
	original, _ := os.ReadFile(editPath)
	_ = os.WriteFile(editPath, append(original, []byte("\ntampered\n")...), 0o644)

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	for _, fa := range plan {
		if fa.Kind == claudeinstall.ActionUpdated {
			// timestamp portion: 2026-05-16T18-32-11Z
			if !strings.Contains(fa.BackupPath, "2026-05-16T18-32-11Z") {
				t.Errorf("backup path %q doesn't contain expected timestamp", fa.BackupPath)
			}
			return
		}
	}
	t.Error("no updated action found")
}

// TestApply_PreCreatesResolvedConfigStub: Apply pre-creates config.resolved.md under
// <targetDir>/.atomic/ when it does not exist yet. This file is @-referenced from
// CLAUDE.md so every Claude session sees it; the file must exist after first install.
func TestApply_PreCreatesResolvedConfigStub(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Plan(target, embedded.Manifest())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := claudeinstall.Apply(target, plan, false, fixedClock); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	resolvedPath := filepath.Join(target, ".atomic", "config.resolved.md")
	if _, err := os.Stat(resolvedPath); err != nil {
		t.Errorf("config.resolved.md not created: %v", err)
	}
}

// TestApply_PreserveExistingResolvedConfig: Apply must not overwrite config.resolved.md
// when it already exists with content. The file is user-managed after first create.
func TestApply_PreserveExistingResolvedConfig(t *testing.T) {
	target := t.TempDir()

	// Pre-create the resolved config with existing content.
	atomicDir := filepath.Join(target, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	resolvedPath := filepath.Join(atomicDir, "config.resolved.md")
	existingContent := []byte("existing\n")
	if err := os.WriteFile(resolvedPath, existingContent, 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	plan, err := claudeinstall.Plan(target, embedded.Manifest())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := claudeinstall.Apply(target, plan, false, fixedClock); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	after, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("read resolved: %v", err)
	}
	if string(after) != string(existingContent) {
		t.Errorf("Apply overwrote config.resolved.md: got %q, want %q", after, existingContent)
	}
}

// TestInstall_CreatesProfileStub: Install creates profile.md under .atomic/ on first install.
// The file must exist and contain all six schema sections.
func TestInstall_CreatesProfileStub(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.InstallWithOutput(target, false, fixedClock, &bytes.Buffer{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	profilePath := filepath.Join(target, ".atomic", "profile.md")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("profile.md not created: %v", err)
	}

	for _, section := range []string{"## Identity", "## Work", "## Active projects", "## Interests", "## People mentioned", "## Environment"} {
		if !strings.Contains(string(data), section) {
			t.Errorf("profile.md missing section %q", section)
		}
	}
}

// TestInstall_ProfileStubIdempotent: Install must not overwrite profile.md when it already exists.
// This ensures user edits are preserved across subsequent installs / updates.
func TestInstall_ProfileStubIdempotent(t *testing.T) {
	target := t.TempDir()

	// Pre-create profile.md with custom user content.
	atomicDir := filepath.Join(target, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	profilePath := filepath.Join(atomicDir, "profile.md")
	userContent := []byte("# My custom profile\n\nPersonal facts.\n")
	if err := os.WriteFile(profilePath, userContent, 0o644); err != nil {
		t.Fatalf("write existing profile.md: %v", err)
	}

	if _, err := claudeinstall.InstallWithOutput(target, false, fixedClock, &bytes.Buffer{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile.md: %v", err)
	}
	if !bytes.Equal(after, userContent) {
		t.Errorf("Install overwrote profile.md: got %q, want %q", after, userContent)
	}
}

// TestInstall_PrintsNudgeOnFirstCreate: Install prints the full bootstrap nudge to stdout
// when profile.md is created for the first time. The exact text is the spec-mandated
// verbatim string from claudeinstall.ProfileNudge — no paraphrasing allowed.
func TestInstall_PrintsNudgeOnFirstCreate(t *testing.T) {
	target := t.TempDir()

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("InstallWithOutput: %v", err)
	}

	if !strings.Contains(buf.String(), claudeinstall.ProfileNudge) {
		t.Errorf("stdout nudge not printed on first install\ngot:  %q\nwant: %q", buf.String(), claudeinstall.ProfileNudge)
	}
}

// TestInstall_SuppressesNudgeWhenAlreadyExists: Install must not print the nudge
// when profile.md already exists (idempotent no-op path).
func TestInstall_SuppressesNudgeWhenAlreadyExists(t *testing.T) {
	target := t.TempDir()

	// Pre-create profile.md so ensureProfileStub is a no-op.
	atomicDir := filepath.Join(target, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(atomicDir, "profile.md"), []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("InstallWithOutput: %v", err)
	}

	const nudge = "Profile created at"
	if strings.Contains(buf.String(), nudge) {
		t.Errorf("nudge must not print when profile.md already exists\nstdout: %q", buf.String())
	}
}

// TestBackupTimestampUsesRunStart: Apply uses the clock captured at the start of the
// run, not the time of the first ActionUpdated. Both items in the plan that are
// Updated must share the same timestamp directory, which must equal the fixed
// clock's value regardless of how many unchanged entries precede them.
func TestBackupTimestampUsesRunStart(t *testing.T) {
	target := t.TempDir()

	// Fresh install first.
	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	// Tamper two artifacts so both are ActionUpdated.
	paths := []string{
		filepath.Join(target, "agents", "atomic-builder.md"),
		filepath.Join(target, "agents", "atomic-reviewer.md"),
	}
	for _, p := range paths {
		orig, _ := os.ReadFile(p)
		_ = os.WriteFile(p, append(orig, []byte("\ntampered\n")...), 0o644)
	}

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	// Collect all backup timestamps.
	seen := map[string]bool{}
	for _, fa := range plan {
		if fa.Kind == claudeinstall.ActionUpdated && fa.BackupPath != "" {
			// Extract the timestamp portion from the path.
			// BackupPath: <target>/.atomic/backups/<timestamp>/<relpath>
			rel := strings.TrimPrefix(fa.BackupPath, target)
			parts := strings.Split(strings.TrimPrefix(rel, string(os.PathSeparator)), string(os.PathSeparator))
			if len(parts) >= 3 {
				seen[parts[2]] = true // parts[0]=.atomic, parts[1]=backups, parts[2]=timestamp
			}
		}
	}
	if len(seen) != 1 {
		t.Errorf("expected all updated actions to share one timestamp dir, got: %v", seen)
	}
	for ts := range seen {
		if ts != "2026-05-16T18-32-11Z" {
			t.Errorf("expected timestamp 2026-05-16T18-32-11Z, got %q", ts)
		}
	}
}
