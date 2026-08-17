package claudeinstall_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/profile"
)

func fixedClock() time.Time {
	return time.Date(2026, 5, 16, 18, 32, 11, 0, time.UTC)
}

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

func TestInstallIntoEmptyTarget(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
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

	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Errorf(".atomic/proposed/CLAUDE.md should not exist on fresh install")
	}
}

func TestInstallSecondRunAllUnchanged(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	unchanged := countKind(plan, claudeinstall.ActionUnchanged)
	if unchanged != len(plan) {
		t.Errorf("second run: unchanged = %d, want %d", unchanged, len(plan))
	}
}

func TestInstallUpdatesChangedArtifact(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	editPath := filepath.Join(target, "agents", "atomic-reviewer.md")
	original, _ := os.ReadFile(editPath)
	tampered := append(original, []byte("\ntampered\n")...)
	if err := os.WriteFile(editPath, tampered, 0o644); err != nil {
		t.Fatalf("write tampered: %v", err)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	updated := countKind(plan, claudeinstall.ActionUpdated)
	if updated != 1 {
		t.Errorf("updated = %d, want 1", updated)
	}

	var updatedAction *claudeinstall.FileAction
	for i := range plan {
		if plan[i].Kind == claudeinstall.ActionUpdated {
			updatedAction = &plan[i]
		}
	}
	if updatedAction == nil {
		t.Fatal("no updated action in plan")
	}

	backupData, err := os.ReadFile(updatedAction.BackupPath)
	if err != nil {
		t.Fatalf("backup %s: %v", updatedAction.BackupPath, err)
	}
	if sha256hex(backupData) != sha256hex(tampered) {
		t.Errorf("backup content doesn't match tampered content")
	}

	onDisk, _ := os.ReadFile(editPath)
	embedded := readEmbedded(t, "bundle/agents/atomic-reviewer.md")
	if sha256hex(onDisk) != sha256hex(embedded) {
		t.Errorf("on-disk file not restored to embedded content")
	}
}

func TestInstallCLAUDEmdDiffers(t *testing.T) {
	target := t.TempDir()

	claudePath := filepath.Join(target, "CLAUDE.md")
	userContent := []byte("# My custom CLAUDE.md\n\nCustom content.\n")
	if err := os.WriteFile(claudePath, userContent, 0o644); err != nil {
		t.Fatalf("write user CLAUDE.md: %v", err)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	mergeRequired := countKind(plan, claudeinstall.ActionMergeRequired)
	if mergeRequired != 1 {
		t.Errorf("merge_required = %d, want 1", mergeRequired)
	}

	current, _ := os.ReadFile(claudePath)
	if sha256hex(current) != sha256hex(userContent) {
		t.Errorf("CLAUDE.md was modified; should be untouched")
	}

	proposedPath := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	proposed, err := os.ReadFile(proposedPath)
	if err != nil {
		t.Fatalf("proposed file: %v", err)
	}
	embeddedClaude := readEmbedded(t, "bundle/CLAUDE.md")
	if sha256hex(proposed) != sha256hex(embeddedClaude) {
		t.Errorf("proposed file does not match embedded CLAUDE.md")
	}

	// Dropping or renaming this string leaves users with no way to finish the merge.
	report := claudeinstall.Report(plan, target)
	if !strings.Contains(report, "atomic prompt claude-merge") {
		t.Errorf("Report output missing 'atomic prompt claude-merge' next-step instruction:\n%s", report)
	}
}

func TestInstallCLAUDEmdIdentical(t *testing.T) {
	target := t.TempDir()

	embeddedClaude := readEmbedded(t, "bundle/CLAUDE.md")
	claudePath := filepath.Join(target, "CLAUDE.md")
	if err := os.WriteFile(claudePath, embeddedClaude, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	for _, fa := range plan {
		if fa.Artifact.Target == "CLAUDE.md" {
			if fa.Kind != claudeinstall.ActionUnchanged {
				t.Errorf("CLAUDE.md action = %s, want unchanged", fa.Kind)
			}
		}
	}

	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Errorf(".atomic/proposed/CLAUDE.md should not exist when CLAUDE.md is unchanged")
	}
}

func TestDryRunNoWrites(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Install(target, target, true /* dryRun */, fixedClock)
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}

	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed == 0 {
		t.Errorf("dry-run plan has zero installs — unexpected")
	}

	entries, _ := os.ReadDir(target)
	if len(entries) != 0 {
		t.Errorf("dry-run wrote files: %v", entries)
	}
}

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

func TestListTabSeparated(t *testing.T) {
	rows := claudeinstall.List()
	for _, r := range rows {
		if r.Kind == "" || r.Target == "" || r.SHA256 == "" {
			t.Errorf("empty field in row: %+v", r)
		}
	}
}

func TestDiffAllAbsent(t *testing.T) {
	target := t.TempDir()

	rows, err := claudeinstall.Diff(target, target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, r := range rows {
		if r.Status != claudeinstall.DiffAbsent {
			t.Errorf("%s: status = %s, want absent", r.Artifact.Target, r.Status)
		}
	}
}

func TestDiffAllMatch(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	rows, err := claudeinstall.Diff(target, target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	for _, r := range rows {
		if r.Status != claudeinstall.DiffMatch {
			t.Errorf("%s: status = %s, want match", r.Artifact.Target, r.Status)
		}
	}
}

func TestDiffMixed(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}

	absentTarget := "agents/atomic-investigator.md"
	if err := os.Remove(filepath.Join(target, absentTarget)); err != nil {
		t.Fatalf("remove: %v", err)
	}

	diffTarget := "agents/atomic-reviewer.md"
	diffPath := filepath.Join(target, diffTarget)
	existing, _ := os.ReadFile(diffPath)
	_ = os.WriteFile(diffPath, append(existing, []byte("\ntampered\n")...), 0o644)

	rows, err := claudeinstall.Diff(target, target)
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
	for _, r := range rows {
		if r.Artifact.Target == absentTarget || r.Artifact.Target == diffTarget {
			continue
		}
		if r.Status != claudeinstall.DiffMatch {
			t.Errorf("%s: want match, got %s", r.Artifact.Target, r.Status)
		}
	}
}

func TestManifestSHAMatchesEmbedded(t *testing.T) {
	for _, a := range embedded.Manifest() {
		data := readEmbedded(t, a.Source)
		actual := sha256hex(data)
		if actual != a.SHA256 {
			t.Errorf("%s: manifest SHA = %s, actual = %s", a.Source, a.SHA256, actual)
		}
	}
}

func TestUpdate_DelegatesToInstall(t *testing.T) {
	target := t.TempDir()

	plan, err := claudeinstall.Update(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	manifest := embedded.Manifest()
	if len(plan) != len(manifest) {
		t.Fatalf("Update plan len = %d, want %d", len(plan), len(manifest))
	}

	installed := countKind(plan, claudeinstall.ActionInstalled)
	if installed != len(manifest) {
		t.Errorf("Update installed = %d, want %d (all)", installed, len(manifest))
	}

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

func TestBackupPathContainsTimestamp(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	editPath := filepath.Join(target, "agents", "atomic-reviewer.md")
	original, _ := os.ReadFile(editPath)
	_ = os.WriteFile(editPath, append(original, []byte("\ntampered\n")...), 0o644)

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	for _, fa := range plan {
		if fa.Kind == claudeinstall.ActionUpdated {
			if !strings.Contains(fa.BackupPath, "2026-05-16T18-32-11Z") {
				t.Errorf("backup path %q doesn't contain expected timestamp", fa.BackupPath)
			}
			return
		}
	}
	t.Error("no updated action found")
}

func TestInstall_CreatesProfileStub(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &bytes.Buffer{}); err != nil {
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

// The refresh seam is no-op'd so this isolates stub idempotency from the
// env-section rewrite, covered by TestInstall_ProfileRefreshCalledAfterStub.
func TestInstall_ProfileStubIdempotent(t *testing.T) {
	target := t.TempDir()

	claudeinstall.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		return false, nil
	}
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })

	atomicDir := filepath.Join(target, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	profilePath := filepath.Join(atomicDir, "profile.md")
	userContent := []byte("# My custom profile\n\nPersonal facts.\n")
	if err := os.WriteFile(profilePath, userContent, 0o644); err != nil {
		t.Fatalf("write existing profile.md: %v", err)
	}

	if _, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &bytes.Buffer{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile.md: %v", err)
	}
	if !bytes.Equal(after, userContent) {
		t.Errorf("ensureProfileStub overwrote profile.md: got %q, want %q", after, userContent)
	}
}

// The nudge text must match ProfileNudge verbatim — no paraphrasing.
func TestInstall_PrintsNudgeOnFirstCreate(t *testing.T) {
	target := t.TempDir()

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("InstallWithOutput: %v", err)
	}

	if !strings.Contains(buf.String(), claudeinstall.ProfileNudge) {
		t.Errorf("stdout nudge not printed on first install\ngot:  %q\nwant: %q", buf.String(), claudeinstall.ProfileNudge)
	}
}

func TestInstall_SuppressesNudgeWhenAlreadyExists(t *testing.T) {
	target := t.TempDir()

	claudeinstall.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		return false, nil
	}
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })

	atomicDir := filepath.Join(target, ".atomic")
	if err := os.MkdirAll(atomicDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(atomicDir, "profile.md"), []byte("existing\n"), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("InstallWithOutput: %v", err)
	}

	const nudge = "Profile created at"
	if strings.Contains(buf.String(), nudge) {
		t.Errorf("nudge must not print when profile.md already exists\nstdout: %q", buf.String())
	}
}

// Install must populate the env fingerprint on first install. The seam lets this
// exercise the wiring without real detection or disk writes.
func TestInstall_ProfileRefreshCalledAfterStub(t *testing.T) {
	target := t.TempDir()

	now := time.Date(2026, 5, 29, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	var gotClaudeHome, gotToday string
	var gotDays int
	claudeinstall.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		gotClaudeHome = claudeHome
		gotToday = today
		gotDays = days
		return false, nil
	}
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })

	var buf bytes.Buffer
	if _, err := claudeinstall.InstallWithOutput(target, target, false, clock, &buf); err != nil {
		t.Fatalf("InstallWithOutput: %v", err)
	}

	if gotDays != profile.DefaultRefreshDays {
		t.Errorf("profileRefresh called with days=%d, want profile.DefaultRefreshDays=%d", gotDays, profile.DefaultRefreshDays)
	}
	wantToday := now.Format("2006-01-02")
	if gotToday != wantToday {
		t.Errorf("profileRefresh called with today=%q, want %q", gotToday, wantToday)
	}
	if gotClaudeHome != target {
		t.Errorf("profileRefresh called with claudeHome=%q, want %q", gotClaudeHome, target)
	}
}

// Install must not fail because detection failed; the stub must still be present.
func TestInstall_ProfileRefreshError_BestEffort(t *testing.T) {
	target := t.TempDir()

	claudeinstall.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		return false, fmt.Errorf("simulated detection failure")
	}
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("Install returned error despite best-effort refresh: %v", err)
	}

	profilePath := filepath.Join(target, ".atomic", "profile.md")
	if _, statErr := os.Stat(profilePath); statErr != nil {
		t.Errorf("profile.md not present after best-effort error: %v", statErr)
	}
}

// Install must not crash even if detection panics; the stub must still be present.
func TestInstall_ProfileRefreshPanic_BestEffort(t *testing.T) {
	target := t.TempDir()

	claudeinstall.ProfileRefresh = func(claudeHome, today string, days int) (bool, error) {
		panic("simulated detection panic")
	}
	prevProfileRefresh := claudeinstall.ProfileRefresh
	t.Cleanup(func() { claudeinstall.ProfileRefresh = prevProfileRefresh })

	var buf bytes.Buffer
	_, err := claudeinstall.InstallWithOutput(target, target, false, fixedClock, &buf)
	if err != nil {
		t.Fatalf("Install returned error despite panic recovery: %v", err)
	}

	profilePath := filepath.Join(target, ".atomic", "profile.md")
	if _, statErr := os.Stat(profilePath); statErr != nil {
		t.Errorf("profile.md not present after panic recovery: %v", statErr)
	}
}

// The env block is populated at install time, so "Claude will fill it in" misleads.
func TestInstall_NudgeNoLongerClaimsClaudeFillsIt(t *testing.T) {
	if strings.Contains(claudeinstall.ProfileNudge, "Claude will fill it in") {
		t.Errorf("ProfileNudge still contains 'Claude will fill it in': %q", claudeinstall.ProfileNudge)
	}
}

// A stale nudge would point users at a verb that no longer exists.
func TestInstall_NudgePointsToRetrospectiveLearning(t *testing.T) {
	if !strings.Contains(claudeinstall.ProfileNudge, "/retrospective-learning") {
		t.Errorf("ProfileNudge does not mention /retrospective-learning: %q", claudeinstall.ProfileNudge)
	}
	if strings.Contains(claudeinstall.ProfileNudge, "/atomic-improve") {
		t.Errorf("ProfileNudge still mentions stale /atomic-improve: %q", claudeinstall.ProfileNudge)
	}
}

// Every backup in one run shares the run-start timestamp, not the time of the
// first ActionUpdated, however many unchanged entries precede it.
func TestBackupTimestampUsesRunStart(t *testing.T) {
	target := t.TempDir()

	if _, err := claudeinstall.Install(target, target, false, fixedClock); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	paths := []string{
		filepath.Join(target, "agents", "atomic-builder.md"),
		filepath.Join(target, "agents", "atomic-reviewer.md"),
	}
	for _, p := range paths {
		orig, _ := os.ReadFile(p)
		_ = os.WriteFile(p, append(orig, []byte("\ntampered\n")...), 0o644)
	}

	plan, err := claudeinstall.Install(target, target, false, fixedClock)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}

	seen := map[string]bool{}
	for _, fa := range plan {
		if fa.Kind == claudeinstall.ActionUpdated && fa.BackupPath != "" {
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
