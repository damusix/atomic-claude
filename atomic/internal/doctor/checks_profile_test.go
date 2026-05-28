package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

func profileBlock() string {
	return "\n## User profile\n\n" + doctor.ProfileRef + "\n"
}

func writeProfileFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PASS: profile.md exists and @-ref present in CLAUDE.md.
func TestCheckProfile_FileAndRefPresent_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), "# Profile\n")
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// WARN: @-ref present, but profile.md file is absent.
func TestCheckProfile_RefPresent_FileMissing_Warn(t *testing.T) {
	claudeHome := t.TempDir()
	// no .atomic/profile.md
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

// WARN: profile.md file present, but @-ref absent from all candidates.
func TestCheckProfile_FilePresent_RefMissing_Warn(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), "# Profile\n")
	// CLAUDE.md exists but contains no ref
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), "# Hello\nno ref here\n")

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

// WARN: both file and @-ref absent.
func TestCheckProfile_BothAbsent_Warn(t *testing.T) {
	claudeHome := t.TempDir()
	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

// PASS: @-ref wired in claude.local.md (not CLAUDE.md).
func TestCheckProfile_RefInClaudeLocalMd_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), "# Profile\n")
	writeProfileFile(t, filepath.Join(claudeHome, "claude.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// PASS: @-ref wired in CLAUDE.local.md variant.
func TestCheckProfile_RefInCLAUDELocalMd_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), "# Profile\n")
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}
