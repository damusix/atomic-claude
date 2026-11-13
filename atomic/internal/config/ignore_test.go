package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitRepoIgnoring is a work tree whose .gitignore holds rules, with a tracked
// file that a rule also matches.
func gitRepoIgnoring(t *testing.T, rules string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGitCheckIgnore(t *testing.T) {
	repo := gitRepoIgnoring(t, "/hidden/\n")
	if ignored, determined := GitCheckIgnore(repo, "hidden/x"); !ignored || !determined {
		t.Errorf("ignored path = %v, %v; want ignored and determined", ignored, determined)
	}
	if ignored, determined := GitCheckIgnore(repo, "shown/x"); ignored || !determined {
		t.Errorf("unignored path = %v, %v; want not ignored but determined", ignored, determined)
	}
	if _, determined := GitCheckIgnore(t.TempDir(), "x"); determined {
		t.Error("a directory outside any work tree reads as determined")
	}
}

func TestGitIgnoreSource(t *testing.T) {
	repo := gitRepoIgnoring(t, "/hidden/\n!/kept/\n")
	if source, ok, err := GitIgnoreSource(repo, "hidden/x"); err != nil || !ok || filepath.Base(source) != ".gitignore" {
		t.Errorf("ignored path source = %q, %v, %v; want the .gitignore", source, ok, err)
	}
	if _, ok, err := GitIgnoreSource(repo, "kept/x"); ok || err != nil {
		t.Errorf("a path a negation re-includes = ok %v, err %v; want neither", ok, err)
	}
	if _, ok, err := GitIgnoreSource(repo, "shown/x"); ok || err != nil {
		t.Errorf("an unmatched path = ok %v, err %v; want neither", ok, err)
	}
	if _, _, err := GitIgnoreSource(t.TempDir(), "x"); err == nil {
		t.Error("a directory outside any work tree reported no error")
	}
}

func TestGitIgnoredSet(t *testing.T) {
	repo := gitRepoIgnoring(t, "/hidden/\n")
	got := GitIgnoredSet(repo, []string{"hidden/x", "shown/x"})
	if !got["hidden/x"] || got["shown/x"] || len(got) != 1 {
		t.Errorf("ignored set = %v, want only hidden/x", got)
	}
	if got := GitIgnoredSet(t.TempDir(), []string{"x"}); got == nil || len(got) != 0 {
		t.Errorf("outside a work tree = %v, want a non-nil empty set", got)
	}
}

// AppendIgnoreRule creates the file, never duplicates a rule, and joins a rule
// to a file that lacks a trailing newline without losing the last line.
func TestAppendIgnoreRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", ".gitignore")
	for range 2 {
		if err := AppendIgnoreRule(path, "/*.db"); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(path); string(got) != "/*.db\n" {
		t.Errorf("fresh file = %q, want one rule", got)
	}

	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AppendIgnoreRule(path, "/*.db"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "keep\n/*.db\n" {
		t.Errorf("appended = %q, want the existing line kept", got)
	}
}

// The header explains a file Atomic created; an existing file is the user's,
// so it never gains one.
func TestAppendIgnoreRuleWithHeader(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh", ".gitignore")
	if wrote, err := AppendIgnoreRuleWithHeader(fresh, "/worktrees/", "# managed\n"); err != nil || !wrote {
		t.Fatalf("fresh append = %v, %v; want a write", wrote, err)
	}
	if got, _ := os.ReadFile(fresh); string(got) != "# managed\n/worktrees/\n" {
		t.Errorf("fresh file = %q, want header then rule", got)
	}
	if wrote, err := AppendIgnoreRuleWithHeader(fresh, "/worktrees/", "# managed\n"); err != nil || wrote {
		t.Errorf("repeat append = %v, %v; want no write", wrote, err)
	}

	existing := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(existing, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendIgnoreRuleWithHeader(existing, "/worktrees/", "# managed\n"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "keep\n/worktrees/\n" {
		t.Errorf("existing file = %q, want no header", got)
	}
}
