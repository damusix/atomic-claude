package doctor_test

import (
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
