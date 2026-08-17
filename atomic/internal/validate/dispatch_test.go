package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

func buildMinimalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	dirs := []string{
		"docs/spec",
		"context/agents",
		"context/commands",
		"context/skills",
		"context/output-styles",
		"context/rules",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../.git/worktrees/test"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	goodSpec := `# Good Spec

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | scaffold | foo.go | passes |

## Change log

<!-- empty -->
`
	if err := os.WriteFile(filepath.Join(root, "docs", "spec", "good-spec.md"), []byte(goodSpec), 0o644); err != nil {
		t.Fatalf("write good-spec.md: %v", err)
	}

	claudeMD := `# CLAUDE.md

Minimal config for test.
`
	if err := os.WriteFile(filepath.Join(root, "context", "CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	return root
}

func addRepoDevMarker(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "atomic", "internal", "bundlemirror")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir bundlemirror: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mirror.go"), []byte("package bundlemirror\n"), 0o644); err != nil {
		t.Fatalf("write mirror.go: %v", err)
	}
}

func runFromDir(t *testing.T, dir string, args []string) (code int, out string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	var buf strings.Builder
	code = validate.RunWithOutput(args, &buf)
	out = buf.String()
	return
}

func TestDispatch_WholeRepo_Exit0(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{})

	if code == 2 {
		t.Errorf("whole-repo on minimal repo: got internal error (exit 2)\noutput:\n%s", out)
	}

	for _, header := range []string{"atomic validate spec", "atomic validate config"} {
		if !strings.Contains(out, header) {
			t.Errorf("whole-repo output missing header %q:\n%s", header, out)
		}
	}
	if strings.Contains(out, "atomic validate bundle") {
		t.Errorf("whole-repo outside atomic-claude repo must skip bundle, but bundle header present:\n%s", out)
	}
}

func TestDispatch_WholeRepo_RepoDev_IncludesBundle(t *testing.T) {
	root := buildMinimalRepo(t)
	addRepoDevMarker(t, root)
	code, out := runFromDir(t, root, []string{})

	if code == 2 {
		t.Errorf("whole-repo on repo-dev tree: got internal error (exit 2)\noutput:\n%s", out)
	}
	for _, header := range []string{"atomic validate spec", "atomic validate config", "atomic validate bundle"} {
		if !strings.Contains(out, header) {
			t.Errorf("repo-dev whole-repo output missing header %q:\n%s", header, out)
		}
	}
}

// Regression: it used to crash with exit 2 instead of skipping.
func TestDispatch_Bundle_NotRepoDev_Skips(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{"bundle"})

	if code != 0 {
		t.Errorf("explicit bundle outside atomic-claude repo: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "SKIP") {
		t.Errorf("explicit bundle skip should mention SKIP:\n%s", out)
	}
}

func TestDispatch_Bundle_NotRepoDev_JSON(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{"--json", "bundle"})

	if code != 0 {
		t.Errorf("explicit bundle --json outside repo: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("bundle --json skip missing schema_version:\n%s", out)
	}
}

func TestDispatch_WholeRepo_JSON(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{"--json"})

	if code == 2 {
		t.Errorf("whole-repo --json: got internal error (exit 2)\noutput:\n%s", out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json output missing schema_version:\n%s", out)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") {
		t.Errorf("--json output does not start with '{': %q", trimmed[:min(50, len(trimmed))])
	}
}

func TestDispatch_WholeRepo_InternalError(t *testing.T) {
	noRepo := t.TempDir() // no .git
	code, out := runFromDir(t, noRepo, []string{})

	if code != 2 {
		t.Errorf("whole-repo outside repo: got exit %d, want 2\noutput:\n%s", code, out)
	}
}

func TestDispatch_PathRouting_SpecPath(t *testing.T) {
	root := buildMinimalRepo(t)

	code, out := runFromDir(t, root, []string{filepath.Join("docs", "spec", "good-spec.md")})
	if code != 0 {
		t.Errorf("path route to spec: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "atomic validate spec") {
		t.Errorf("path route to spec: expected spec header, got:\n%s", out)
	}
}

func TestDispatch_PathRouting_UnknownPath(t *testing.T) {
	root := buildMinimalRepo(t)

	code, out := runFromDir(t, root, []string{"some/unknown/path.txt"})
	if code != 0 {
		t.Errorf("unknown path: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("unknown path: expected WARN finding:\n%s", out)
	}
	if !strings.Contains(out, "no validator applicable") {
		t.Errorf("unknown path: expected 'no validator applicable' in output:\n%s", out)
	}
}

func TestDispatch_PathRouting_AbsolutePath(t *testing.T) {
	root := buildMinimalRepo(t)

	// EvalSymlinks resolves macOS /var → /private/var so that the absolute
	// path we construct matches what os.Getwd() returns after chdir (which
	// Go's os.Getwd uses syscall, resolving symlinks). Without this, Rel()
	// produces a ../../.. path and isSpecPath mismatches.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = root
	}

	absPath := filepath.Join(realRoot, "docs", "spec", "good-spec.md")
	code, out := runFromDir(t, root, []string{absPath})
	if code != 0 {
		t.Errorf("absolute path route to spec: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "atomic validate spec") {
		t.Errorf("absolute path route to spec: expected spec header, got:\n%s", out)
	}
	// "WARN  dispatch" is the finding signature for wrong routing. The summary
	// line "0 WARN" is a false-positive match, so check for the finding form.
	if strings.Contains(out, "WARN  dispatch") {
		t.Errorf("absolute path route to spec: got WARN dispatch finding (wrong routing), want spec validation:\n%s", out)
	}
}

func TestDispatch_PathRouting_MixedPaths(t *testing.T) {
	root := buildMinimalRepo(t)

	specPath := filepath.Join("docs", "spec", "good-spec.md")
	designPath := filepath.Join("docs", "design", "something.md")
	code, out := runFromDir(t, root, []string{specPath, designPath})

	if code != 0 {
		t.Errorf("mixed paths: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("mixed paths: expected WARN for design path:\n%s", out)
	}
}

func TestPerfBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in -short mode")
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := findRepoRootForTest(cwd)
	if root == "" {
		t.Skipf("no .git found walking up from %s; skipping perf test", cwd)
	}

	start := time.Now()
	code, out := runFromDir(t, root, []string{})
	elapsed := time.Since(start)

	if code != 0 && code != 1 {
		t.Fatalf("unexpected exit code %d during perf test:\n%s", code, out)
	}

	const budget = 500 * time.Millisecond
	if elapsed > budget {
		t.Errorf("perf budget exceeded: whole-repo validate took %v (budget %v)", elapsed, budget)
	}
	t.Logf("whole-repo validate elapsed: %v", elapsed)
}

// findRepoRootForTest duplicates the package-internal findRepoRoot because this
// is an external _test package.
func findRepoRootForTest(startDir string) string {
	dir := startDir
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
