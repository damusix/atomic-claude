package mdlink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkFile(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestLinkifyFile_GitignoreSeam verifies a token reported ignored stays plain
// while a normal resolvable token still links.
func TestLinkifyFile_GitignoreSeam(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "build-output.log")
	mkFile(t, dir, "agents/atomic-builder.md")

	orig := gitIgnored
	gitIgnored = func(base string, tokens []string) map[string]bool {
		return map[string]bool{"build-output.log": true}
	}
	defer func() { gitIgnored = orig }()

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "ignored `build-output.log`, linked `agents/atomic-builder.md`.\n"
	got := LinkifyFile(content, fileAbs, dir)

	if !strings.Contains(got, "`build-output.log`") || strings.Contains(got, "](../../build-output.log)") {
		t.Errorf("ignored token should stay plain, got: %q", got)
	}
	if !strings.Contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("normal token should link, got: %q", got)
	}
}

// TestLinkifyFile_NonGitDir verifies graceful degradation: in a non-git dir the
// real gitignore check returns nothing, so resolvable tokens link as usual.
func TestLinkifyFile_NonGitDir(t *testing.T) {
	dir := t.TempDir() // not a git repo
	mkFile(t, dir, "agents/atomic-builder.md")

	fileAbs := filepath.Join(dir, ".claude", "project", "signals.md")
	content := "see `agents/atomic-builder.md`.\n"
	got := LinkifyFile(content, fileAbs, dir)

	if !strings.Contains(got, "[`agents/atomic-builder.md`](../../agents/atomic-builder.md)") {
		t.Errorf("non-git dir should still link, got: %q", got)
	}
}

// TestExtractTokens verifies inline spans are collected and fenced blocks skipped.
func TestExtractTokens(t *testing.T) {
	content := "prose `a/b.go` and `c`.\n```\n`fenced/skip.go`\n```\ntail `d/e.md`.\n"
	got := extractTokens(content)
	want := []string{"a/b.go", "c", "d/e.md"}
	if len(got) != len(want) {
		t.Fatalf("token count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %q want %q", i, got[i], want[i])
		}
	}
}
