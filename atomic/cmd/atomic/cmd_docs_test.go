package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/docs"
)

// A misconfigured import path or switch fall-through would silently produce
// no output, so these go through the dispatch switch, not docs.Scan.
func TestRunDocsScanDispatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "index.md"), []byte("# Index\n\n## Intro\n"), 0o644); err != nil {
		t.Fatalf("write index.md: %v", err)
	}

	code := docsAction([]string{"scan"}, root)
	if code != 0 {
		t.Fatalf("docsAction(scan) returned exit code %d, want 0", code)
	}

	cachePath := filepath.Join(root, ".claude", "project", "doc-surfaces.md")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not written by docsAction(scan): %v", err)
	}
	if !strings.Contains(string(data), "docs/index.md") {
		t.Errorf("cache missing 'docs/index.md'; got:\n%s", string(data))
	}
}

// Exit codes are the contract for CI consumers: nil→0, ErrStale→1, other→2.
func TestRunDocsStaleDispatch(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{"stale"}, root)
	if code != 2 {
		t.Fatalf("docsAction(stale) with no cache: got exit code %d, want 2", code)
	}

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "guide.md"), []byte("# Guide\n"), 0o644); err != nil {
		t.Fatalf("write guide.md: %v", err)
	}
	if err := docs.Scan(root); err != nil {
		t.Fatalf("docs.Scan: %v", err)
	}

	code = docsAction([]string{"stale"}, root)
	if code != 0 {
		t.Errorf("docsAction(stale) after fresh scan: got exit code %d, want 0", code)
	}
}

// Every dispatch function returns non-zero for a missing verb; a zero here
// would make bare `atomic docs` silently succeed.
func TestRunDocsNoSubcommandUsage(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{}, root)
	if code != 1 {
		t.Errorf("docsAction with no args: got exit code %d, want 1", code)
	}
}

// An unknown verb must not fall through to a silent no-op.
func TestRunDocsUnknownVerbDispatch(t *testing.T) {
	root := t.TempDir()

	code := docsAction([]string{"bogus"}, root)
	if code != 1 {
		t.Errorf("docsAction(bogus): got exit code %d, want 1", code)
	}
}

func writeDocsIndexDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	files := map[string]string{
		"sources.md": "---\ntitle: Sources\ndescription: Register a source.\n---\n\nBody.\n",
		"jobs.md":    "---\ntitle: Jobs\ndescription: Read the queue.\n---\n\nBody.\n",
		"index.md":   "# Help\n\n<bucket-docs>\n\n## Docs\n\n- stale\n\n</bucket-docs>\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestRunDocsIndexDispatch(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "admin")
	writeDocsIndexDir(t, dir)

	var code int
	stdout, _ := captureOutput(t, func() { code = docsAction([]string{"index", dir}, t.TempDir()) })
	if code != 0 {
		t.Fatalf("docsAction(index) returned %d, want 0", code)
	}
	if want := dir + ": 2 indexed, 0 unindexed\n"; stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
	}
}

func TestRunDocsIndexCheckExitCodes(t *testing.T) {
	base := t.TempDir()
	stale := filepath.Join(base, "stale")
	writeDocsIndexDir(t, stale)
	missing := filepath.Join(base, "missing")

	before, err := os.ReadFile(filepath.Join(stale, "index.md"))
	if err != nil {
		t.Fatal(err)
	}

	var code int
	stdout, _ := captureOutput(t, func() { code = docsAction([]string{"index", "--check", stale}, base) })
	if code != 1 {
		t.Errorf("--check stale: exit %d, want 1", code)
	}
	if want := "STALE " + filepath.Join(stale, "index.md") + "\n"; stdout != want {
		t.Errorf("--check stale: stdout %q, want %q", stdout, want)
	}
	after, err := os.ReadFile(filepath.Join(stale, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("--check must not write index.md")
	}

	stdout, stderr := captureOutput(t, func() { code = docsAction([]string{"index", "--check", stale, missing}, base) })
	if code != 2 {
		t.Errorf("--check stale missing: exit %d, want 2", code)
	}
	if !strings.Contains(stdout, "STALE "+filepath.Join(stale, "index.md")) {
		t.Errorf("stale dir must still be reported alongside the error: stdout %q", stdout)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("stderr must name the missing dir: %q", stderr)
	}

	_, stderr = captureOutput(t, func() { code = docsAction([]string{"index", missing}, base) })
	if code != 1 {
		t.Errorf("write mode, missing dir: exit %d, want 1", code)
	}
	if !strings.Contains(stderr, missing) {
		t.Errorf("stderr must name the missing dir: %q", stderr)
	}

	captureOutput(t, func() { code = docsAction([]string{"index", stale}, base) })
	if code != 0 {
		t.Fatalf("write run: exit %d, want 0", code)
	}
	stdout, _ = captureOutput(t, func() { code = docsAction([]string{"index", "--check", stale}, base) })
	if code != 0 || stdout != "" {
		t.Errorf("--check after write: exit %d stdout %q, want 0 and no output", code, stdout)
	}
}

func TestRunDocsNoVerbUsageNamesIndex(t *testing.T) {
	_, stderr := captureOutput(t, func() { docsAction(nil, t.TempDir()) })
	if want := "Usage: atomic docs <scan|stale|index>\n"; stderr != want {
		t.Errorf("stderr %q, want %q", stderr, want)
	}
}

func TestRunDocsIndexFlagAfterDirIsUsageError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "admin")
	writeDocsIndexDir(t, dir)
	before, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}

	var code int
	stdout, _ := captureOutput(t, func() { code = docsAction([]string{"index", dir, "--check"}, t.TempDir()) })
	if code != 2 {
		t.Errorf("flag after dir: exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("flag after dir: stdout %q, want none", stdout)
	}
	after, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("a usage error must write nothing")
	}
}

func TestRunDocsIndexDashDirAfterTerminator(t *testing.T) {
	base := t.TempDir()
	writeDocsIndexDir(t, filepath.Join(base, "-admin"))
	t.Chdir(base)

	var code int
	stdout, stderr := captureOutput(t, func() { code = docsAction([]string{"index", "--", "-admin"}, base) })
	if code != 0 {
		t.Fatalf("-- -admin: exit %d, want 0; stderr %q", code, stderr)
	}
	if want := "-admin: 2 indexed, 0 unindexed\n"; stdout != want {
		t.Errorf("stdout %q, want %q", stdout, want)
	}
}
