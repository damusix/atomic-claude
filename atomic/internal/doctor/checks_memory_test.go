package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeMemorySetup writes MEMORY.md for a project and returns the base dir the
// caller passes as claudeHome.
func makeMemorySetup(t *testing.T, project, content string) string {
	t.Helper()
	base := t.TempDir()
	memDir := filepath.Join(base, "projects", project, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	return base
}

// These expectations mirror Claude Code's own slugification: every
// non-alphanumeric character becomes "-", per character, and case is kept.
func TestProjectMemoryDirDerivation(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/Users/alonso/projects/github/claude-code-setup", "-Users-alonso-projects-github-claude-code-setup"},
		{"/home/user/repo", "-home-user-repo"},
		{"/tmp/x", "-tmp-x"},
		// Both "/" and "." convert, so a dotted segment doubles the dash.
		{"/Users/alonso/.claude", "-Users-alonso--claude"},
		{"/Users/alonso/projects/pi-os/.worktrees/x", "-Users-alonso-projects-pi-os--worktrees-x"},
		// A drive letter leaves no stray leading dash.
		{`C:\Users\master-user\Documents\Projects\vibe0\vibe-core`, "C--Users-master-user-Documents-Projects-vibe0-vibe-core"},
	}
	for _, tc := range tests {
		got := doctor.ProjectNameFromCWD(tc.cwd)
		if got != tc.want {
			t.Errorf("ProjectNameFromCWD(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestCheckMemoryFileAbsent(t *testing.T) {
	claudeHome := t.TempDir()
	project := "-tmp-testproject"
	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS", r.Severity)
	}
}

func TestCheckMemoryAllResolve(t *testing.T) {
	project := "-tmp-testproject"
	content := "# Persistent Agent Memory\n\n- [Topic A](topic_a.md)\n- [Topic B](topic_b.md)\n"
	claudeHome := makeMemorySetup(t, project, content)

	memDir := filepath.Join(claudeHome, "projects", project, "memory")
	for _, name := range []string{"topic_a.md", "topic_b.md"} {
		if err := os.WriteFile(filepath.Join(memDir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
}

func TestCheckMemoryOneOrphan(t *testing.T) {
	project := "-tmp-testproject"
	content := "# Persistent Agent Memory\n\n- [Topic A](topic_a.md)\n- [Missing](missing.md)\n"
	claudeHome := makeMemorySetup(t, project, content)

	memDir := filepath.Join(claudeHome, "projects", project, "memory")
	if err := os.WriteFile(filepath.Join(memDir, "topic_a.md"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write topic_a.md: %v", err)
	}
	// missing.md intentionally absent.

	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
}

// Counting absolute-path and URL targets would over-report the denominator.
func TestCheckMemoryAllResolve_ExcludesSkippedTargets(t *testing.T) {
	project := "-tmp-testproject"
	content := "# Persistent Agent Memory\n\n- [Topic A](topic_a.md)\n- [Absolute](/absolute/path.md)\n- [External](https://example.com/file.md)\n"
	claudeHome := makeMemorySetup(t, project, content)

	memDir := filepath.Join(claudeHome, "projects", project, "memory")
	if err := os.WriteFile(filepath.Join(memDir, "topic_a.md"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write topic_a.md: %v", err)
	}

	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS (detail: %s)", r.Severity, r.Detail)
	}
	// "1/1", not "1/3".
	if r.Detail != "1/1 refs resolve" {
		t.Errorf("detail = %q, want %q", r.Detail, "1/1 refs resolve")
	}
}

// Past three orphans the detail truncates with an ellipsis.
func TestCheckMemoryManyOrphans(t *testing.T) {
	project := "-tmp-testproject"
	content := "# Persistent Agent Memory\n\n- [A](a.md)\n- [B](b.md)\n- [C](c.md)\n- [D](d.md)\n"
	claudeHome := makeMemorySetup(t, project, content)
	// No target file is written.

	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.WARN {
		t.Errorf("severity = %v, want WARN", r.Severity)
	}
	found := false
	for i := 0; i < len(r.Detail)-2; i++ {
		if r.Detail[i:i+3] == "..." {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("detail %q: expected '...' for 4 orphans", r.Detail)
	}
}
