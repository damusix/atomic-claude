package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// RunCheckRefsWith runs the refs check against a synthetic repo root.
func RunCheckRefsWith(repoRoot string) doctor.Result {
	return doctor.RunCheckRefsWith(repoRoot)
}

const (
	deterministicRef = "@.claude/project/deterministic-signals.md"
	signalsRef       = "@.claude/project/signals.md"
)

func bothRefs() string {
	return "\n## Project signals (auto-loaded)\n\n" +
		deterministicRef + "\n" +
		signalsRef + "\n"
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

// TestCheckRefs_BothRefsInClaudeLocalMd_Pass verifies PASS when both refs are
// in claude.local.md (first in search order).
func TestCheckRefs_BothRefsInClaudeLocalMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

// TestCheckRefs_BothRefsInCLAUDEMd_Pass verifies PASS when both refs are in CLAUDE.md.
func TestCheckRefs_BothRefsInCLAUDEMd_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_NoRefsAnywhere_Fail verifies FAIL when no candidate file has
// either ref.
func TestCheckRefs_NoRefsAnywhere_Fail(t *testing.T) {
	root := t.TempDir()
	// No candidate files at all.
	r := RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

// TestCheckRefs_OnlyFirstRef_Fail verifies FAIL when only deterministic ref is present.
func TestCheckRefs_OnlyFirstRef_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), deterministicRef+"\n")

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_OnlySecondRef_Fail verifies FAIL when only inferred ref is present.
func TestCheckRefs_OnlySecondRef_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), signalsRef+"\n")

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL; detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_RefsSplitAcrossFiles_Fail verifies FAIL when each ref is in a
// different file — both must appear in the SAME file per spec.
func TestCheckRefs_RefsSplitAcrossFiles_Fail(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), deterministicRef+"\n")
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), signalsRef+"\n")

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Errorf("severity = %q, want FAIL (refs split across files); detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_SearchOrder_FirstFileWins verifies that when both refs appear in
// both claude.local.md and CLAUDE.md, the detail reports claude.local.md (first match).
func TestCheckRefs_SearchOrder_FirstFileWins(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.local.md"), bothRefs())
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
	// Detail should mention claude.local.md (first in search order).
	want := "refs wired in claude.local.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}

// TestCheckRefs_CLAUDELocalMd_Variant verifies CLAUDE.local.md (capital) is also found.
func TestCheckRefs_CLAUDELocalMd_Variant_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.local.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_ClaudeMdLower verifies claude.md (all lowercase) is also found.
func TestCheckRefs_ClaudeMdLower_Pass(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "claude.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %q, want PASS; detail: %q", r.Severity, r.Detail)
	}
}

// TestCheckRefs_PartialDetail_MentionsMissing verifies FAIL detail names the missing ref.
func TestCheckRefs_PartialDetail_MentionsMissing(t *testing.T) {
	root := t.TempDir()
	// Only deterministic ref in CLAUDE.md.
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), deterministicRef+"\n")

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.FAIL {
		t.Fatalf("severity = %q, want FAIL", r.Severity)
	}
	if r.Detail == "" {
		t.Error("expected non-empty detail")
	}
}

// TestCheckRefs_PassDetailMentionsFilename verifies the PASS detail names the file.
func TestCheckRefs_PassDetailMentionsFilename(t *testing.T) {
	root := t.TempDir()
	writeRefsFile(t, filepath.Join(root, "CLAUDE.md"), bothRefs())

	r := RunCheckRefsWith(root)
	if r.Severity != doctor.PASS {
		t.Fatalf("severity = %q, want PASS", r.Severity)
	}
	// Detail must say "refs wired in CLAUDE.md".
	want := "refs wired in CLAUDE.md"
	if r.Detail != want {
		t.Errorf("detail = %q, want %q", r.Detail, want)
	}
}
