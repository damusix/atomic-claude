package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// freshProfile returns a profile.md body with a lastcheck set to today's date.
// The staleness check uses time.Now() inside RunCheckProfileWith, so "today"
// here matches that clock — guaranteed fresh.
func freshProfile() string {
	today := time.Now().Format("2006-01-02")
	return "# User profile\n\n## Environment\n<deterministic lastcheck=" + today + ">\n- OS: linux\n</deterministic>\n"
}

// staleProfile returns a profile.md body with lastcheck fixed to 2000-01-01
// (always stale relative to any real today).
func staleProfile() string {
	return "# User profile\n\n## Environment\n<deterministic lastcheck=2000-01-01>\n- OS: linux\n</deterministic>\n"
}

// v1Profile returns a profile.md body without a lastcheck attribute (v1 format).
func v1Profile() string {
	return "# User profile\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
}

// PASS: profile.md exists and @-ref present in CLAUDE.md.
func TestCheckProfile_FileAndRefPresent_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), freshProfile())
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
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), freshProfile())
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
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(claudeHome, "claude.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// PASS: @-ref wired in CLAUDE.local.md variant.
func TestCheckProfile_RefInCLAUDELocalMd_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// WARN: profile.md present and ref wired, but lastcheck is stale (2000-01-01).
// Detail must contain the lastcheck date and guidance to run `atomic profile refresh`.
func TestCheckProfile_StaleLastcheck_Warn(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), staleProfile())
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "2000-01-01") {
		t.Errorf("detail should contain lastcheck date '2000-01-01'; got: %q", r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic profile refresh") {
		t.Errorf("detail should mention 'atomic profile refresh'; got: %q", r.Detail)
	}
}

// WARN: profile.md exists but is unreadable (permissions).
// Must report "unreadable", not "absent".
func TestCheckProfile_FileUnreadable_Warn(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 does not restrict access")
	}

	claudeHome := t.TempDir()
	profilePath := filepath.Join(claudeHome, ".atomic", "profile.md")
	writeProfileFile(t, profilePath, freshProfile())
	if err := os.Chmod(profilePath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(profilePath, 0o644) })

	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "unreadable") {
		t.Errorf("detail should say 'unreadable'; got: %q", r.Detail)
	}
	if strings.Contains(r.Detail, "absent") {
		t.Errorf("detail must not say 'absent' for an unreadable file; got: %q", r.Detail)
	}
}

// WARN: profile.md present and ref wired, but no lastcheck attribute (v1 format).
// Detail must mention "atomic profile refresh" so the user knows how to fix it.
func TestCheckProfile_AbsentLastcheck_Warn(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), v1Profile())
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic profile refresh") {
		t.Errorf("detail should mention 'atomic profile refresh'; got: %q", r.Detail)
	}
}

// PASS: profile.md present, ref wired, and lastcheck is today (fresh).
func TestCheckProfile_FreshLastcheck_Pass(t *testing.T) {
	claudeHome := t.TempDir()
	writeProfileFile(t, filepath.Join(claudeHome, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(claudeHome, "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(claudeHome)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}
