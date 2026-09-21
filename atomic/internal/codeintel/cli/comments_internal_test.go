package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func noStdinComments() io.Reader { return strings.NewReader("") }

func writeFileComments(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeDiff builds a synthetic `git diff -U0` fragment adding lines to a new
// file, so the classification table needs no git process.
func makeDiff(path string, start int, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	b.WriteString("--- /dev/null\n")
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -0,0 +%d,%d @@\n", start, len(lines))
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func TestParseAddedComments_ClassificationTable(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		lines    []string
		wantN    int
		wantText string
	}{
		{"slash group (go)", "x.go", []string{"// Greet returns a greeting."}, 1, "Greet returns a greeting."},
		{"hash group (py)", "x.py", []string{"# comment here"}, 1, "comment here"},
		{"dash group (sql)", "x.sql", []string{"-- comment"}, 1, "comment"},
		{"percent group (erl)", "x.erl", []string{"% comment"}, 1, "comment"},
		{"markup group (html)", "x.html", []string{"<!-- comment -->"}, 1, "comment"},
		{"style group scss line prefix", "x.scss", []string{"// scss line comment"}, 1, "scss line comment"},
		{"unknown extension skipped", "x.foo", []string{"// not counted"}, 0, ""},
		{"md skipped", "x.md", []string{"// not counted"}, 0, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diff := makeDiff(tc.path, 1, tc.lines)
			got := parseAddedComments(diff, 2)
			if len(got) != tc.wantN {
				t.Fatalf("got %d entries, want %d: %+v", len(got), tc.wantN, got)
			}
			if tc.wantN == 1 {
				if got[0].Path != tc.path {
					t.Errorf("path = %q, want %q", got[0].Path, tc.path)
				}
				if got[0].Line != 1 {
					t.Errorf("line = %d, want 1", got[0].Line)
				}
				if got[0].Text != tc.wantText {
					t.Errorf("text = %q, want %q", got[0].Text, tc.wantText)
				}
			}
		})
	}
}

func TestParseAddedComments_GoDirectiveNotComment(t *testing.T) {
	diff := makeDiff("x.go", 1, []string{"//go:build linux"})
	got := parseAddedComments(diff, 2)
	if len(got) != 0 {
		t.Fatalf("expected //go: directive to be skipped, got: %+v", got)
	}
}

func TestParseAddedComments_ShebangNotComment(t *testing.T) {
	diff := makeDiff("x.sh", 1, []string{"#!/bin/bash"})
	got := parseAddedComments(diff, 2)
	if len(got) != 0 {
		t.Fatalf("expected shebang to be skipped, got: %+v", got)
	}
}

func TestParseAddedComments_BlockComment(t *testing.T) {
	diff := makeDiff("x.go", 1, []string{"/* line one", " * line two", " */"})
	got := parseAddedComments(diff, 2)
	if len(got) != 1 {
		t.Fatalf("expected 1 block entry, got: %+v", got)
	}
	if got[0].Lines != 3 {
		t.Errorf("lines = %d, want 3", got[0].Lines)
	}
	if !got[0].Over {
		t.Errorf("expected 3-line block to exceed max_lines 2")
	}
}

func TestFirstWords_TruncatesOnRuneBoundary(t *testing.T) {
	s := strings.Repeat("a", 58) + "€€€" + strings.Repeat("b", 10)
	got := firstWords(s)
	if !utf8.ValidString(got) {
		t.Fatalf("firstWords produced invalid UTF-8: %q", got)
	}
	want := strings.Repeat("a", 58) + "€€"
	if got != want {
		t.Errorf("firstWords = %q, want %q", got, want)
	}
}

func TestParseAddedComments_LineGapSplitsEntries(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/x.go b/x.go\n")
	b.WriteString("+++ b/x.go\n")
	b.WriteString("@@ -0,0 +1,1 @@\n")
	b.WriteString("+// comment one\n")
	b.WriteString("@@ -10,0 +11,1 @@\n")
	b.WriteString("+// comment two\n")

	got := parseAddedComments(b.String(), 2)
	if len(got) != 2 {
		t.Fatalf("expected a line-number gap to split into 2 entries, got: %+v", got)
	}
	if got[0].Line != 1 || got[1].Line != 11 {
		t.Errorf("unexpected line numbers: %+v", got)
	}
}

func gitFixtureComments(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	runGitComments(t, dir, "init")
	runGitComments(t, dir, "config", "user.email", "test@example.com")
	runGitComments(t, dir, "config", "user.name", "Test")
	return dir
}

func runGitComments(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestRunComments_WorkingTree_TrackedAndUntracked(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")

	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\n// Entry point.\nfunc main() {}\n")
	writeFileComments(t, filepath.Join(dir, "new.go"), "package main\n\n// New helper.\nfunc helper() {}\n")

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments"}, dir, &stdout, &stderr, noStdinComments())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "main.go") || !strings.Contains(out, "new.go") {
		t.Errorf("expected both tracked and untracked files counted, got: %s", out)
	}
	if !strings.Contains(out, "comments added: 2") {
		t.Errorf("expected 2 comments, got: %s", out)
	}
}

func TestRunComments_CommitRange_UntrackedNotCounted(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")
	base := strings.TrimSpace(runGitComments(t, dir, "rev-parse", "HEAD"))

	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\n// Entry point.\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "add comment")

	writeFileComments(t, filepath.Join(dir, "untracked.go"), "package main\n\n// Should not count.\nfunc x() {}\n")

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments", "--diff", base + "..HEAD"}, dir, &stdout, &stderr, noStdinComments())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, "untracked.go") {
		t.Errorf("untracked file counted in a commit range: %s", out)
	}
	if !strings.Contains(out, "comments added: 1") {
		t.Errorf("expected 1 comment, got: %s", out)
	}
}

func TestRunComments_UntrackedReadError_WarnsAndContinues(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")

	brokenPath := filepath.Join(dir, "broken.go")
	writeFileComments(t, brokenPath, "package main\n")
	if err := os.Chmod(brokenPath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(brokenPath, 0o644) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits don't block reads")
	}

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments"}, dir, &stdout, &stderr, noStdinComments())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "broken.go") {
		t.Errorf("expected stderr to name broken.go, got: %s", stderr.String())
	}
}

func TestRunComments_ExitCode1_OverMax(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")

	writeFileComments(t, filepath.Join(dir, ".claude", "atomic.toml"), "[comments]\nmax_lines = 1\n")

	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\n// line one\n// line two\nfunc main() {}\n")

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments"}, dir, &stdout, &stderr, noStdinComments())
	if code != 1 {
		t.Fatalf("expected exit 1, got %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "OVER") {
		t.Errorf("expected OVER marker, got: %s", stdout.String())
	}
}

func TestRunComments_ExitCode2_BadRange(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments", "--diff", "not-a-real-ref"}, dir, &stdout, &stderr, noStdinComments())
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%s", code, stderr.String())
	}
	if stderr.String() == "" {
		t.Errorf("expected error on stderr")
	}
}

func TestRunComments_JSONShape(t *testing.T) {
	dir := gitFixtureComments(t)
	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	runGitComments(t, dir, "add", ".")
	runGitComments(t, dir, "commit", "-m", "init")

	writeFileComments(t, filepath.Join(dir, "main.go"), "package main\n\n// Entry point.\nfunc main() {}\n")

	var stdout, stderr bytes.Buffer
	code := RunCode([]string{"comments", "--json"}, dir, &stdout, &stderr, noStdinComments())
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", code, stderr.String())
	}

	var payload struct {
		Diff     string `json:"diff"`
		MaxLines int    `json:"max_lines"`
		Added    int    `json:"added"`
		Over     int    `json:"over"`
		Comments []struct {
			Path  string `json:"path"`
			Line  int    `json:"line"`
			Lines int    `json:"lines"`
			Text  string `json:"text"`
			Over  bool   `json:"over"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nstdout=%s", err, stdout.String())
	}
	if payload.Diff != "HEAD" || payload.MaxLines != 2 || payload.Added != 1 || payload.Over != 0 {
		t.Errorf("unexpected payload: %+v", payload)
	}
	if len(payload.Comments) != 1 || payload.Comments[0].Path != "main.go" {
		t.Errorf("unexpected comments: %+v", payload.Comments)
	}
}
