package doctor_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
)

// makeMemoryFile writes MEMORY.md at dotClaudeProjects/<project>/memory/MEMORY.md.
// Returns the dotClaudeProjects base dir (caller uses it as the claudeHome prefix).
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

// TestProjectMemoryDirDerivation verifies the project name from cwd path.
// Rule: mirror Claude Code's slugification — every non-alphanumeric character
// (path separators "/" and "\", the Windows drive colon ":", dots, etc.) is
// replaced by "-", per character. A POSIX leading "/" yields a leading "-";
// existing hyphens and letter case are preserved.
func TestProjectMemoryDirDerivation(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/Users/alonso/projects/github/claude-code-setup", "-Users-alonso-projects-github-claude-code-setup"},
		{"/home/user/repo", "-home-user-repo"},
		{"/tmp/x", "-tmp-x"},
		// Dotted segment: "/.claude" → "--claude" (both "/" and "." become "-").
		// Matches Claude Code's real dir name for ~/.claude (-Users-alonso--claude).
		{"/Users/alonso/.claude", "-Users-alonso--claude"},
		{"/Users/alonso/projects/pi-os/.worktrees/x", "-Users-alonso-projects-pi-os--worktrees-x"},
		// Windows path (issue #43): drive colon and backslashes slugify too, with
		// no stray leading "-". C:\...\vibe-core → C--Users-...-vibe-core.
		{`C:\Users\master-user\Documents\Projects\vibe0\vibe-core`, "C--Users-master-user-Documents-Projects-vibe0-vibe-core"},
	}
	for _, tc := range tests {
		got := doctor.ProjectNameFromCWD(tc.cwd)
		if got != tc.want {
			t.Errorf("ProjectNameFromCWD(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

// TestCheckMemoryFileAbsent verifies PASS when no MEMORY.md exists.
func TestCheckMemoryFileAbsent(t *testing.T) {
	claudeHome := t.TempDir()
	project := "-tmp-testproject"
	r := doctor.RunCheckMemoryWith(claudeHome, project)
	if r.Severity != doctor.PASS {
		t.Errorf("severity = %v, want PASS", r.Severity)
	}
}

// TestCheckMemoryAllResolve verifies PASS when all links resolve.
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

// TestCheckMemoryOneOrphan verifies WARN when one link target is missing.
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

// TestCheckMemoryAllResolve_ExcludesSkippedTargets verifies that absolute-path
// and URL targets are NOT counted in the N/N refs resolve total. Including them
// would over-report the denominator (e.g. "3/3" when only 1 relative link exists).
func TestCheckMemoryAllResolve_ExcludesSkippedTargets(t *testing.T) {
	project := "-tmp-testproject"
	// MEMORY.md has one relative link (resolves), one absolute path, one URL.
	// Only the relative link should be counted.
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
	// Must be "1/1", not "1/3" (absolute+URL targets excluded from count).
	if r.Detail != "1/1 refs resolve" {
		t.Errorf("detail = %q, want %q", r.Detail, "1/1 refs resolve")
	}
}

// TestCheckMemoryManyOrphans verifies WARN with "..." when more than 3 orphans.
func TestCheckMemoryManyOrphans(t *testing.T) {
	project := "-tmp-testproject"
	content := "# Persistent Agent Memory\n\n- [A](a.md)\n- [B](b.md)\n- [C](c.md)\n- [D](d.md)\n"
	claudeHome := makeMemorySetup(t, project, content)
	// All targets absent.

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
