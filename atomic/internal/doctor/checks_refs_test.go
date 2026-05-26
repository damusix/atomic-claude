package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

const signalsRef = "@.claude/project/signals.md"

func refBlock() string {
	return "\n## Project signals (auto-loaded)\n\n" + signalsRef + "\n"
}

func writeRefsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRefs_RefInClaudeLocalMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_RefInCLAUDEMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_NoRefsAnywhere_Fail(t *testing.T) {
	root := t.TempDir()
	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_OnlyDeterministicRef_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), "@.claude/project/deterministic-signals.md\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_SignalsRefPresent_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), signalsRef+"\n")

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_SearchOrder_FirstFileWins(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), refBlock())
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	want := "ref wired in claude.local.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}

func TestCheckRefs_CLAUDELocalMd_Variant_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.local.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_ClaudeMdLower_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

func TestCheckRefs_PassDetailMentionsFilename(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), refBlock())

	r := doctor.RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS", r.Severity)
	}
	want := "ref wired in CLAUDE.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}
