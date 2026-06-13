package claudeinstall_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// These tests pin the deterministic CLAUDE.md update contract: once a user's
// CLAUDE.md carries an <atomic>...</atomic> block, install/update compares and
// replaces only that block. User content outside the block must never cause
// drift (merge_required / DiffDiffer) and must never be touched on update.
// The LLM merge path (proposed file + `atomic prompt claude-merge`) remains only for
// files without a parseable block.

// mergedCLAUDEmd returns the embedded CLAUDE.md with user content appended
// after the </atomic> block — the shape of a file after a completed merge.
func mergedCLAUDEmd(t *testing.T) string {
	t.Helper()
	return string(readEmbedded(t, "bundle/CLAUDE.md")) + "\n## My custom rules\n\nKeep me intact.\n"
}

// staleBlock rewrites content so its <atomic> block differs from the embedded
// one (simulates a file merged against an older bundle version).
func staleBlock(content string) string {
	return strings.Replace(content, "<atomic>\n", "<atomic>\nstale line from a prior bundle version\n", 1)
}

func writeCLAUDEmd(t *testing.T, target, content string) string {
	t.Helper()
	path := filepath.Join(target, "CLAUDE.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	return path
}

func claudeAction(t *testing.T, plan []claudeinstall.FileAction) claudeinstall.FileAction {
	t.Helper()
	for _, fa := range plan {
		if fa.Artifact.Target == "CLAUDE.md" {
			return fa
		}
	}
	t.Fatal("CLAUDE.md not in plan")
	return claudeinstall.FileAction{}
}

// Same <atomic> block + user content outside → no action, no proposed file.
func TestInstallMergedCLAUDEmdSameBlockUnchanged(t *testing.T) {
	target := t.TempDir()
	content := mergedCLAUDEmd(t)
	path := writeCLAUDEmd(t, target, content)

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if fa := claudeAction(t, plan); fa.Kind != claudeinstall.ActionUnchanged {
		t.Errorf("CLAUDE.md action = %s, want unchanged", fa.Kind)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(got) != content {
		t.Error("CLAUDE.md was modified; same-block file must be untouched")
	}

	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Error("proposed file written for same-block CLAUDE.md; merge must not be requested")
	}
}

// Stale <atomic> block + user content outside → deterministic in-place block
// replacement: block refreshed, user content preserved, original backed up.
func TestInstallMergedCLAUDEmdStaleBlockReplaced(t *testing.T) {
	target := t.TempDir()
	content := staleBlock(mergedCLAUDEmd(t))
	path := writeCLAUDEmd(t, target, content)

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	fa := claudeAction(t, plan)
	if fa.Kind != claudeinstall.ActionBlockReplaced {
		t.Fatalf("CLAUDE.md action = %s, want block_replaced", fa.Kind)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	want := mergedCLAUDEmd(t)
	if string(got) != want {
		t.Errorf("post-update CLAUDE.md mismatch:\n got %d bytes\nwant %d bytes (embedded block + preserved user content)", len(got), len(want))
	}
	if !strings.Contains(string(got), "## My custom rules") {
		t.Error("user content outside <atomic> was lost")
	}
	if strings.Contains(string(got), "stale line from a prior bundle version") {
		t.Error("stale block content survived; block must be replaced wholesale")
	}

	// Original backed up under the shared run timestamp.
	backup := filepath.Join(target, ".atomic", "backups", "2026-05-16T18-32-11Z", "CLAUDE.md")
	backed, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backed) != content {
		t.Error("backup does not match pre-update CLAUDE.md")
	}
	if fa.BackupPath != backup {
		t.Errorf("BackupPath = %q, want %q", fa.BackupPath, backup)
	}

	proposed := filepath.Join(target, ".atomic", "proposed", "CLAUDE.md")
	if _, err := os.Stat(proposed); !os.IsNotExist(err) {
		t.Error("proposed file written; block replacement must not request a merge")
	}
}

// Dry run plans block_replaced but writes nothing.
func TestInstallMergedCLAUDEmdStaleBlockDryRun(t *testing.T) {
	target := t.TempDir()
	content := staleBlock(mergedCLAUDEmd(t))
	path := writeCLAUDEmd(t, target, content)

	plan, err := claudeinstall.Install(target, true, fixedClock)
	if err != nil {
		t.Fatalf("Install dry-run: %v", err)
	}
	if fa := claudeAction(t, plan); fa.Kind != claudeinstall.ActionBlockReplaced {
		t.Errorf("CLAUDE.md action = %s, want block_replaced", fa.Kind)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if string(got) != content {
		t.Error("dry run modified CLAUDE.md")
	}
}

// Diff reports match for a merged file whose block is current — this is what
// keeps doctor check 1 green on already-merged installs.
func TestDiffMergedCLAUDEmdSameBlockMatches(t *testing.T) {
	target := t.TempDir()
	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}
	writeCLAUDEmd(t, target, mergedCLAUDEmd(t))

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, r := range rows {
		if r.Artifact.Target == "CLAUDE.md" {
			if r.Status != claudeinstall.DiffMatch {
				t.Errorf("CLAUDE.md diff status = %s, want match", r.Status)
			}
			return
		}
	}
	t.Fatal("CLAUDE.md not in diff rows")
}

// Diff still reports differ when the block itself is stale.
func TestDiffMergedCLAUDEmdStaleBlockDiffers(t *testing.T) {
	target := t.TempDir()
	if _, err := claudeinstall.Install(target, false, fixedClock); err != nil {
		t.Fatalf("Install: %v", err)
	}
	writeCLAUDEmd(t, target, staleBlock(mergedCLAUDEmd(t)))

	rows, err := claudeinstall.Diff(target)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, r := range rows {
		if r.Artifact.Target == "CLAUDE.md" {
			if r.Status != claudeinstall.DiffDiffer {
				t.Errorf("CLAUDE.md diff status = %s, want diff", r.Status)
			}
			return
		}
	}
	t.Fatal("CLAUDE.md not in diff rows")
}

// Report renders block-replaced actions with the preservation note.
func TestReportRendersBlockReplaced(t *testing.T) {
	target := t.TempDir()
	writeCLAUDEmd(t, target, staleBlock(mergedCLAUDEmd(t)))

	plan, err := claudeinstall.Install(target, false, fixedClock)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	report := claudeinstall.Report(plan, target)
	if !strings.Contains(report, "CLAUDE.md") || !strings.Contains(report, "<atomic>") {
		t.Errorf("report missing block-replacement line for CLAUDE.md:\n%s", report)
	}
	if strings.Contains(report, "Needs review") {
		t.Errorf("report requests a merge review for a deterministic block replacement:\n%s", report)
	}
}
