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

// freshProfile stamps today's date, matching the real clock the check reads.
func freshProfile() string {
	today := time.Now().Format("2006-01-02")
	return "# User profile\n\n## Environment\n<deterministic lastcheck=" + today + ">\n- OS: linux\n</deterministic>\n"
}

// staleProfile pins a lastcheck stale against any real today.
func staleProfile() string {
	return "# User profile\n\n## Environment\n<deterministic lastcheck=2000-01-01>\n- OS: linux\n</deterministic>\n"
}

// v1Profile predates the lastcheck attribute.
func v1Profile() string {
	return "# User profile\n\n## Environment\n<deterministic>\n- OS: linux\n</deterministic>\n"
}

func TestCheckProfile_FileAndRefPresent_Pass(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_RefPresent_FileMissing_Warn(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_FilePresent_RefMissing_Warn(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), "# Hello\nno ref here\n")

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_BothAbsent_Warn(t *testing.T) {
	home := t.TempDir()
	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_RefInClaudeLocalMd_Pass(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "claude.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_RefInCLAUDELocalMd_Pass(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.local.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckProfile_StaleLastcheck_Warn(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), staleProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
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

// An unreadable file must not be reported as an absent one.
func TestCheckProfile_FileUnreadable_Warn(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: chmod 000 does not restrict access")
	}

	home := t.TempDir()
	profilePath := filepath.Join(home, ".atomic", "profile.md")
	writeProfileFile(t, profilePath, freshProfile())
	if err := os.Chmod(profilePath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(profilePath, 0o644) })

	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
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

func TestCheckProfile_AbsentLastcheck_Warn(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), v1Profile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic profile refresh") {
		t.Errorf("detail should mention 'atomic profile refresh'; got: %q", r.Detail)
	}
}

func TestCheckProfile_FreshLastcheck_Pass(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), profileBlock())

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// The legacy ref still resolves through the compat symlink, so only this
// check tells the user their CLAUDE.md is from an old bundle.
func TestCheckProfile_LegacyRef_Warn(t *testing.T) {
	home := t.TempDir()
	writeProfileFile(t, filepath.Join(home, ".atomic", "profile.md"), freshProfile())
	legacyBlock := "\n## User profile\n\n@~/.claude/.atomic/profile.md\n"
	writeProfileFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), legacyBlock)

	r := doctor.RunCheckProfileWith(home)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %q, want WARN; detail: %q", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "atomic claude install") {
		t.Errorf("detail should mention 'atomic claude install'; got: %q", r.Detail)
	}
}
