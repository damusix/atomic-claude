package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
)

// TestCheckInstall_pass: all embedded artifacts exist with matching SHAs →
// Result severity must be PASS.
func TestCheckInstall_pass(t *testing.T) {
	target := t.TempDir()

	// Write all embedded artifacts to the temp dir with correct content.
	for _, a := range embedded.Manifest() {
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckInstall_warn_drift: one artifact has wrong content → WARN (no
// missing files, just SHA mismatch).
func TestCheckInstall_warn_drift(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	for i, a := range manifest {
		if i == 0 {
			// Write wrong content for the first artifact.
			writeFile(t, filepath.Join(target, filepath.FromSlash(a.Target)), []byte("drift"))
		} else {
			installArtifact(t, target, a)
		}
	}

	r := doctor.RunCheckInstall(target)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("Detail is empty, want drift description")
	}
}

// TestCheckInstall_fail_missing: one artifact is absent → FAIL.
func TestCheckInstall_fail_missing(t *testing.T) {
	target := t.TempDir()

	manifest := embedded.Manifest()
	// Install all except the first.
	for i, a := range manifest {
		if i == 0 {
			continue
		}
		installArtifact(t, target, a)
	}

	r := doctor.RunCheckInstall(target)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckInstall_skip_missing_dir: ~/.claude/ itself doesn't exist → SKIP.
func TestCheckInstall_skip_missing_dir(t *testing.T) {
	target := filepath.Join(t.TempDir(), "nonexistent")

	r := doctor.RunCheckInstall(target)
	if r.Severity != doctor.SKIP {
		t.Errorf("severity = %q, want SKIP; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckInstall_atomic_subtree_not_flagged: files under <claudeHome>/.atomic/
// are NOT in the embedded manifest and must not be flagged as missing or drifted.
// The install check only compares manifest entries vs on-disk; files in .atomic/
// that are not manifest entries should be invisible to the check.
func TestCheckInstall_atomic_subtree_not_flagged(t *testing.T) {
	target := t.TempDir()

	// Write all embedded artifacts (clean install).
	for _, a := range embedded.Manifest() {
		installArtifact(t, target, a)
	}

	// Add .atomic/ files that claudeinstall creates but that are NOT in the manifest.
	atomicFiles := []string{
		filepath.Join(target, ".atomic", "config.resolved.md"),
		filepath.Join(target, ".atomic", "config.toml"),
		filepath.Join(target, ".atomic", "backups", "2026-01-01T00-00-00Z", "CLAUDE.md"),
		filepath.Join(target, ".atomic", "proposed", "CLAUDE.md"),
	}
	for _, f := range atomicFiles {
		writeFile(t, f, []byte("# atomic-owned state"))
	}

	r := doctor.RunCheckInstall(target)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s (atomic subtree must not be flagged)", r.Severity, r.Detail)
	}
}

// --- helpers ---

func installArtifact(t *testing.T, target string, a embedded.Artifact) {
	t.Helper()
	data, err := embedded.FS.ReadFile(a.Source)
	if err != nil {
		t.Fatalf("read embedded %s: %v", a.Source, err)
	}
	writeFile(t, filepath.Join(target, filepath.FromSlash(a.Target)), data)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
