package claudeinstall_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
)

// sha256HexBytes returns the hex-encoded SHA256 of data.
func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeTestManifest writes a minimal pre-install manifest.json into preInstallDir.
func writeTestManifest(t *testing.T, preInstallDir string, files []map[string]interface{}) {
	t.Helper()
	m := map[string]interface{}{
		"created":        time.Now().UTC().Format(time.RFC3339),
		"atomic_version": "1.5.1",
		"files":          files,
	}
	data, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.MkdirAll(preInstallDir, 0o755); err != nil {
		t.Fatalf("mkdir pre-install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(preInstallDir, "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// TestBuildUninstallPlan_MissingManifest verifies an error is returned when no
// manifest exists — the CLI must exit 1 in this case.
func TestBuildUninstallPlan_MissingManifest(t *testing.T) {
	targetDir := t.TempDir()
	_, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
	if !strings.Contains(err.Error(), "no pre-install snapshot") {
		t.Errorf("error %q does not mention 'no pre-install snapshot'", err.Error())
	}
}

// TestBuildUninstallPlan_ExistedTrue verifies existed=true files are placed in
// the Restore list (they must be restored from the pre-install copy).
func TestBuildUninstallPlan_ExistedTrue(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "settings.json", "sha256": "abc123", "existed": true},
		{"path": "CLAUDE.md", "sha256": "def456", "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	if len(plan.Restore) != 2 {
		t.Errorf("Restore count = %d, want 2", len(plan.Restore))
	}
	if len(plan.Delete) != 0 {
		t.Errorf("Delete count = %d, want 0", len(plan.Delete))
	}
}

// TestBuildUninstallPlan_ExistedFalse verifies existed=false files are placed in
// the Delete list (atomic added them; uninstall removes them).
func TestBuildUninstallPlan_ExistedFalse(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "agents/atomic-builder.md", "sha256": "", "existed": false},
		{"path": "agents/atomic-reviewer.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	if len(plan.Delete) != 2 {
		t.Errorf("Delete count = %d, want 2", len(plan.Delete))
	}
	if len(plan.Restore) != 0 {
		t.Errorf("Restore count = %d, want 0", len(plan.Restore))
	}
}

// TestBuildUninstallPlan_MergeDetection verifies NeedsMerge=true when the
// on-disk file has a SHA different from the pre-install snapshot (user modified it).
func TestBuildUninstallPlan_MergeDetection(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// Current on-disk CLAUDE.md — different from pre-install SHA.
	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte("modified content"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	// Snapshot records a different SHA (pre-install state).
	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": "aaaaaa", "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	if len(plan.Restore) != 1 {
		t.Fatalf("Restore count = %d, want 1", len(plan.Restore))
	}
	if !plan.Restore[0].NeedsMerge {
		t.Errorf("CLAUDE.md NeedsMerge = false, want true (on-disk sha differs from pre-install)")
	}
}

// TestBuildUninstallPlan_NoMergeWhenUnchanged verifies NeedsMerge=false when
// the on-disk SHA matches the pre-install SHA (user did not modify the file).
func TestBuildUninstallPlan_NoMergeWhenUnchanged(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	content := []byte("original content")
	sum := sha256HexBytes(content)

	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, content, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": sum, "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	if len(plan.Restore) != 1 {
		t.Fatalf("Restore count = %d, want 1", len(plan.Restore))
	}
	if plan.Restore[0].NeedsMerge {
		t.Errorf("CLAUDE.md NeedsMerge = true, want false (sha matches pre-install)")
	}
}

// TestBuildUninstallPlan_EmbeddedSHA_Delete verifies that when the on-disk file
// matches the embedded bundle SHA (atomic wrote it, user never touched it),
// the file is moved to the Delete list — not Restore — because there is nothing
// to restore for the user.
func TestBuildUninstallPlan_EmbeddedSHA_Delete(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// The on-disk file currently matches the embedded SHA (user never modified it).
	onDiskContent := []byte("atomic-written content")
	embeddedSHA := sha256HexBytes(onDiskContent) // on-disk == embedded → atomic-only
	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, onDiskContent, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	// Pre-install snapshot recorded a different SHA (the file existed before install).
	preInstallSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": preInstallSHA, "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlanWithManifest(targetDir, map[string]string{
		"CLAUDE.md": embeddedSHA,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUninstallPlanWithManifest: %v", err)
	}

	// on-disk SHA == embedded SHA means atomic wrote it and user didn't touch it.
	// Result: Delete (not Restore), because uninstall should remove what atomic wrote.
	if len(plan.Delete) != 1 {
		t.Errorf("Delete count = %d, want 1 (on-disk matches embedded → atomic-only, should delete)", len(plan.Delete))
	}
	if len(plan.Restore) != 0 {
		t.Errorf("Restore count = %d, want 0", len(plan.Restore))
	}
}

// TestBuildUninstallPlan_EmbeddedSHA_NeedsMerge verifies that when the on-disk
// file differs from both the pre-install SHA and the embedded SHA, the file is
// placed in Restore with NeedsMerge=true (user modified it post-install).
func TestBuildUninstallPlan_EmbeddedSHA_NeedsMerge(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	embeddedSHA := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	preInstallSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	// On-disk content produces a SHA that matches neither pre-install nor embedded.
	onDiskContent := []byte("user-modified content post-install")
	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, onDiskContent, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": preInstallSHA, "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlanWithManifest(targetDir, map[string]string{
		"CLAUDE.md": embeddedSHA,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUninstallPlanWithManifest: %v", err)
	}

	if len(plan.Restore) != 1 {
		t.Fatalf("Restore count = %d, want 1", len(plan.Restore))
	}
	if !plan.Restore[0].NeedsMerge {
		t.Errorf("CLAUDE.md NeedsMerge = false, want true (differs from both pre-install and embedded)")
	}
}

// TestBuildUninstallPlan_CurrentMatchesPreInstall verifies that when the on-disk
// file still matches the pre-install SHA (unchanged since install), it is Restored
// without NeedsMerge — a plain copy-back is safe.
func TestBuildUninstallPlan_CurrentMatchesPreInstall(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	content := []byte("original pre-install content")
	preInstallSHA := sha256HexBytes(content)
	embeddedSHA := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, content, 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": preInstallSHA, "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlanWithManifest(targetDir, map[string]string{
		"CLAUDE.md": embeddedSHA,
	}, nil)
	if err != nil {
		t.Fatalf("BuildUninstallPlanWithManifest: %v", err)
	}

	if len(plan.Restore) != 1 {
		t.Fatalf("Restore count = %d, want 1", len(plan.Restore))
	}
	if plan.Restore[0].NeedsMerge {
		t.Errorf("CLAUDE.md NeedsMerge = true, want false (current matches pre-install → unchanged)")
	}
}

// TestGenerateUninstallPrompt_UsesTargetDir verifies that file paths in the
// generated prompt use targetDir, not hardcoded ~/.claude/.
func TestGenerateUninstallPrompt_UsesTargetDir(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "settings.json", "sha256": "abc", "existed": true},
		{"path": "agents/atomic-builder.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	prompt := claudeinstall.GenerateUninstallPrompt(targetDir, plan)

	if strings.Contains(prompt, "~/.claude/") {
		t.Errorf("prompt contains hardcoded ~/.claude/ — should use targetDir %q", targetDir)
	}
	if !strings.Contains(prompt, targetDir) {
		t.Errorf("prompt does not contain targetDir %q", targetDir)
	}
}

// TestGenerateUninstallPrompt_KeyStructure verifies the generated prompt contains
// all mandatory sections and file references.
func TestGenerateUninstallPrompt_KeyStructure(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "settings.json", "sha256": "abc", "existed": true},
		{"path": "agents/atomic-builder.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	prompt := claudeinstall.GenerateUninstallPrompt(targetDir, plan)

	requiredPhrases := []string{
		"## Atomic Claude Uninstall",
		"### Plan",
		"### Instructions",
		".atomic",
		"atomic-builder.md",
		"settings.json",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("prompt missing %q", phrase)
		}
	}
}

// TestBuildUninstallPlan_ProfileMdExcluded verifies that .atomic/profile.md is
// excluded from both the Restore and Delete lists even when a synthetic manifest
// includes it with Existed=false. Profile is user-data written post-install by
// ensureProfileStub; uninstall must never delete it.
func TestBuildUninstallPlan_ProfileMdExcluded(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// Synthetic manifest that includes profile.md (Existed=false → would normally Delete).
	// Also include a normal atomic artifact so we can verify Delete still works for others.
	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": ".atomic/profile.md", "sha256": "", "existed": false},
		{"path": "agents/atomic-builder.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	// profile.md must not appear in Delete.
	for _, p := range plan.Delete {
		if p == ".atomic/profile.md" {
			t.Errorf("Delete list contains .atomic/profile.md — must be excluded to preserve user data")
		}
	}

	// profile.md must not appear in Restore either.
	for _, r := range plan.Restore {
		if r.RelPath == ".atomic/profile.md" {
			t.Errorf("Restore list contains .atomic/profile.md — must be excluded to preserve user data")
		}
	}

	// Normal atomic artifact should still be in Delete.
	found := false
	for _, p := range plan.Delete {
		if p == "agents/atomic-builder.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Delete list missing agents/atomic-builder.md — non-profile entries should still be deleted")
	}
}

// TestBuildUninstallPlan_ProfileMdExcluded_ExistedTrue verifies that .atomic/profile.md
// is excluded from both Restore and Delete even when the manifest entry has Existed=true
// with a populated SHA. The guard fires before the Existed branch, so both paths are
// covered: this test locks the Existed=true coverage; TestBuildUninstallPlan_ProfileMdExcluded
// covers Existed=false.
func TestBuildUninstallPlan_ProfileMdExcluded_ExistedTrue(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	// Synthetic manifest: profile.md with Existed=true and a real SHA.
	// Without the guard this would land in Restore (existed=true path).
	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": ".atomic/profile.md", "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "existed": true},
		{"path": "agents/atomic-builder.md", "sha256": "", "existed": false},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	// profile.md must not appear in Restore.
	for _, r := range plan.Restore {
		if r.RelPath == ".atomic/profile.md" {
			t.Errorf("Restore list contains .atomic/profile.md (Existed=true) — guard must fire before Existed branch")
		}
	}

	// profile.md must not appear in Delete.
	for _, p := range plan.Delete {
		if p == ".atomic/profile.md" {
			t.Errorf("Delete list contains .atomic/profile.md (Existed=true) — guard must fire before Existed branch")
		}
	}

	// Other entries are unaffected.
	found := false
	for _, p := range plan.Delete {
		if p == "agents/atomic-builder.md" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Delete list missing agents/atomic-builder.md — non-profile entries should still be deleted")
	}
}

// TestGenerateUninstallPrompt_NeedsMergeLabel verifies the NEEDS MERGE label
// appears in the prompt for files where on-disk SHA differs from pre-install.
func TestGenerateUninstallPrompt_NeedsMergeLabel(t *testing.T) {
	targetDir := t.TempDir()
	preInstallDir := filepath.Join(targetDir, ".atomic", "pre-install")

	claudeMDPath := filepath.Join(targetDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMDPath, []byte("modified post-install"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	writeTestManifest(t, preInstallDir, []map[string]interface{}{
		{"path": "CLAUDE.md", "sha256": "stale-sha", "existed": true},
	})

	plan, err := claudeinstall.BuildUninstallPlan(targetDir)
	if err != nil {
		t.Fatalf("BuildUninstallPlan: %v", err)
	}

	prompt := claudeinstall.GenerateUninstallPrompt(targetDir, plan)
	if !strings.Contains(prompt, "NEEDS MERGE") {
		t.Errorf("prompt missing 'NEEDS MERGE' label for modified CLAUDE.md")
	}
}
