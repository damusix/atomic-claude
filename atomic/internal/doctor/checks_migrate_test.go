package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// TestCheckMigrateDrift_olderInstall: binary newer than install version → WARN nudging migrate.
func TestCheckMigrateDrift_olderInstall(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[install]\nversion = \"0.1.0\"\n")

	r := doctor.RunCheckMigrateDriftWith(root, "1.0.0")
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "migration") && !strings.Contains(r.Detail, "pending") {
		t.Errorf("detail %q: want mention of pending migration", r.Detail)
	}
	if !strings.Contains(r.Remediation, "migrate") {
		t.Errorf("remediation %q: want 'atomic migrate'", r.Remediation)
	}
}

// TestCheckMigrateDrift_equalVersions: binary == install version → PASS.
func TestCheckMigrateDrift_equalVersions(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[install]\nversion = \"1.0.0\"\n")

	r := doctor.RunCheckMigrateDriftWith(root, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_newerInstall: install version > binary → PASS (no nudge when install is ahead).
func TestCheckMigrateDrift_newerInstall(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[install]\nversion = \"2.0.0\"\n")

	r := doctor.RunCheckMigrateDriftWith(root, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_noInstallTable: config.toml present but no [install] section → PASS (pre-framework).
func TestCheckMigrateDrift_noInstallTable(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[output.signals]\nmax_depth = 3\n")

	r := doctor.RunCheckMigrateDriftWith(root, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (pre-framework install); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_noConfigTOML: no config.toml → PASS (not installed via atomic).
func TestCheckMigrateDrift_noConfigTOML(t *testing.T) {
	root := t.TempDir()
	// Deliberately do NOT write config.toml.

	r := doctor.RunCheckMigrateDriftWith(root, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (no config.toml); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_devBinary: binary version "dev" floors to 0.0.0 → no nudge.
func TestCheckMigrateDrift_devBinary(t *testing.T) {
	root := t.TempDir()
	// Any valid semver install version is >= "dev" (0.0.0).
	writeTOML(t, root, "[install]\nversion = \"0.0.1\"\n")

	r := doctor.RunCheckMigrateDriftWith(root, "dev")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for dev binary; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_devBinaryAny: "dev" against higher version also no nudge.
func TestCheckMigrateDrift_devBinaryAny(t *testing.T) {
	root := t.TempDir()
	writeTOML(t, root, "[install]\nversion = \"5.3.0\"\n")

	r := doctor.RunCheckMigrateDriftWith(root, "dev")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS for dev binary vs any install version; detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_legacyStateDir_absent: no ~/.claude/.atomic at all
// (fresh machine, or migration never had anything to move) → PASS.
func TestCheckMigrateDrift_legacyStateDir_absent(t *testing.T) {
	home := t.TempDir()
	// Deliberately no config.toml and no ~/.claude/.atomic.

	r := doctor.RunCheckMigrateDriftWith(home, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (legacy dir absent); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_legacyStateDir_symlink: ~/.claude/.atomic is the
// compat symlink left by a completed migration → PASS.
func TestCheckMigrateDrift_legacyStateDir_symlink(t *testing.T) {
	home := t.TempDir()
	newDir := filepath.Join(home, ".atomic")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.atomic: %v", err)
	}
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.claude: %v", err)
	}
	if err := os.Symlink(newDir, filepath.Join(claudeDir, ".atomic")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	r := doctor.RunCheckMigrateDriftWith(home, "1.0.0")
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS (legacy dir is the compat symlink); detail: %s", r.Severity, r.Detail)
	}
}

// TestCheckMigrateDrift_legacyStateDir_realDir: ~/.claude/.atomic is still a
// real directory — migration to ~/.atomic hasn't completed on this machine → WARN.
func TestCheckMigrateDrift_legacyStateDir_realDir(t *testing.T) {
	home := t.TempDir()
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}

	r := doctor.RunCheckMigrateDriftWith(home, "1.0.0")
	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN (real legacy dir); detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, ".claude/.atomic") {
		t.Errorf("detail %q: want mention of the legacy path", r.Detail)
	}
	if !strings.Contains(r.Detail, "automatically") {
		t.Errorf("detail %q: want mention that migration runs automatically on any verb", r.Detail)
	}
}

// TestCheckMigrateDrift_versionDriftAndLegacyDir_combinedWARN: both
// conditions present at once → single combined-detail WARN Result (mirrors
// checks_config.go's combined-WARN style), severity is the worst of the two,
// and the version-drift Remediation survives the merge.
func TestCheckMigrateDrift_versionDriftAndLegacyDir_combinedWARN(t *testing.T) {
	home := t.TempDir()
	writeTOML(t, home, "[install]\nversion = \"0.1.0\"\n")
	legacyDir := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}

	r := doctor.RunCheckMigrateDriftWith(home, "1.0.0")
	if r.Severity != doctor.WARN {
		t.Fatalf("severity = %q, want WARN; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "pending") {
		t.Errorf("detail %q: want the version-drift condition's detail", r.Detail)
	}
	if !strings.Contains(r.Detail, ".claude/.atomic") {
		t.Errorf("detail %q: want the legacy-dir condition's detail", r.Detail)
	}
	if !strings.Contains(r.Remediation, "migrate") {
		t.Errorf("remediation %q: want 'atomic migrate' preserved from the version-drift condition", r.Remediation)
	}
}
