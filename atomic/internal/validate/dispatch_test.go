package validate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/validate"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildMinimalRepo creates a synthetic repo under dir with the minimal
// structure required for spec + config + bundle validators to return exit 0.
//
// Structure:
//   - docs/spec/good-spec.md — a valid spec (passes S0/S1/S5/S6)
//   - CLAUDE.md              — minimal, references no agents
//   - agents/                — empty dir (no agents to check)
//   - commands/              — empty dir
//   - skills/                — empty dir
//   - output-styles/         — empty dir
//   - rules/                 — empty dir
//   - .git                   — file (simulates a git worktree)
func buildMinimalRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	dirs := []string{
		"docs/spec",
		"agents",
		"commands",
		"skills",
		"output-styles",
		"rules",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// .git as a file (simulates a worktree; findRepoRoot handles both).
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../.git/worktrees/test"), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	// A valid spec that passes all S-rules.
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

	// Minimal CLAUDE.md with no @-refs and no agent registry entries.
	claudeMD := `# CLAUDE.md

Minimal config for test.
`
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}

	return root
}

// runFromDir temporarily sets the working directory and calls RunWithOutput,
// then restores the original wd. This is needed so findRepoRoot works correctly.
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

// ---------------------------------------------------------------------------
// TestDispatch_WholeRepo
// ---------------------------------------------------------------------------

// TestDispatch_WholeRepo_Exit0 proves that `atomic validate` (no args) on a
// minimal repo runs ALL three validators (evidenced by all three headers) and
// exits with a valid code (0 or 1 — not 2).
//
// WHY: CP-8 replaces the "subcommand required" stub with real whole-repo
// dispatch. The key contract is: all three validator sections run and produce
// output. Exit 0 or 1 both indicate validators ran; exit 2 means the dispatch
// itself failed (internal error), which must not happen on a valid repo.
//
// Note: the synthetic repo has no real bundle artifacts, so bundle parity
// will FAIL (exit 1 is expected). The test checks that all three sections ran,
// not that everything passed.
func TestDispatch_WholeRepo_Exit0(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{})

	// exit 2 = internal error: dispatch itself failed — must not happen.
	if code == 2 {
		t.Errorf("whole-repo on minimal repo: got internal error (exit 2)\noutput:\n%s", out)
	}

	// Each subcommand header must appear — proving all three validators ran.
	for _, header := range []string{"atomic validate spec", "atomic validate config", "atomic validate bundle"} {
		if !strings.Contains(out, header) {
			t.Errorf("whole-repo output missing header %q:\n%s", header, out)
		}
	}
}

// TestDispatch_WholeRepo_JSON proves `atomic validate --json` emits a single
// JSON envelope containing findings from ALL validators (schema_version:1).
//
// WHY: JSON consumers must receive one envelope, not three separate JSON blobs.
// Exit 0 or 1 both valid (synthetic repo may fail bundle parity). Exit 2 must
// not occur — that is an internal dispatch error.
func TestDispatch_WholeRepo_JSON(t *testing.T) {
	root := buildMinimalRepo(t)
	code, out := runFromDir(t, root, []string{"--json"})

	if code == 2 {
		t.Errorf("whole-repo --json: got internal error (exit 2)\noutput:\n%s", out)
	}
	if !strings.Contains(out, "schema_version") {
		t.Errorf("--json output missing schema_version:\n%s", out)
	}
	// Must be valid-ish JSON (starts with '{').
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "{") {
		t.Errorf("--json output does not start with '{': %q", trimmed[:min(50, len(trimmed))])
	}
}

// TestDispatch_WholeRepo_InternalError proves that whole-repo dispatch exits 2
// when findRepoRoot cannot find a .git (i.e. not in a repo).
//
// WHY: Internal error path must be loud (exit 2), not silently exit 0.
func TestDispatch_WholeRepo_InternalError(t *testing.T) {
	noRepo := t.TempDir() // no .git
	code, out := runFromDir(t, noRepo, []string{})

	if code != 2 {
		t.Errorf("whole-repo outside repo: got exit %d, want 2\noutput:\n%s", code, out)
	}
}

// ---------------------------------------------------------------------------
// TestDispatch_PathRouting
// ---------------------------------------------------------------------------

// TestDispatch_PathRouting_SpecPath proves that `atomic validate docs/spec/foo.md`
// routes to the spec validator (runs S-rules on the file) rather than erroring.
//
// WHY: CP-8 path-aware routing — docs/spec/*.md must reach the spec runner.
func TestDispatch_PathRouting_SpecPath(t *testing.T) {
	root := buildMinimalRepo(t)

	// Run with the path to the good spec.
	code, out := runFromDir(t, root, []string{filepath.Join("docs", "spec", "good-spec.md")})
	if code != 0 {
		t.Errorf("path route to spec: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "atomic validate spec") {
		t.Errorf("path route to spec: expected spec header, got:\n%s", out)
	}
}

// TestDispatch_PathRouting_UnknownPath proves that `atomic validate <unknown>`
// emits a WARN finding for paths that have no applicable validator.
//
// WHY: spec § CP-8: unknown paths → WARN, not FAIL or exit 2.
func TestDispatch_PathRouting_UnknownPath(t *testing.T) {
	root := buildMinimalRepo(t)

	code, out := runFromDir(t, root, []string{"some/unknown/path.txt"})
	// Exit 0 (WARN is not FAIL).
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

// TestDispatch_PathRouting_AbsolutePath proves that an absolute path argument
// to `atomic validate` routes correctly to the spec validator rather than
// falling through to WARN.
//
// WHY: CP-8 reviewer flag — filepath.Join(repoRoot, absPath) does NOT strip
// the root on Unix; it concatenates, producing a double-rooted path whose
// Rel() result is wrong. The fix detects IsAbs first and calls Rel directly.
// Without the fix, docs/spec/foo.md passed as an absolute path gets WARNed
// instead of validated.
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

	// Construct the absolute path to the good spec inside the temp repo.
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

// TestDispatch_PathRouting_MixedPaths proves that a mixed-path invocation routes
// spec paths to spec and unknown paths to WARN, all in one run.
//
// WHY: users may pass mixed file lists from git status or CI matchers.
func TestDispatch_PathRouting_MixedPaths(t *testing.T) {
	root := buildMinimalRepo(t)

	specPath := filepath.Join("docs", "spec", "good-spec.md")
	designPath := filepath.Join("docs", "design", "something.md")
	code, out := runFromDir(t, root, []string{specPath, designPath})

	// Exit 0 (spec passes; design → WARN only).
	if code != 0 {
		t.Errorf("mixed paths: got exit %d, want 0\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "WARN") {
		t.Errorf("mixed paths: expected WARN for design path:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestPerfBudget
// ---------------------------------------------------------------------------

// TestPerfBudget proves that `atomic validate` (whole-repo) on this repo
// completes in under 500ms.
//
// WHY: spec § "Soft perf budget: <500ms on a modern machine." Performance
// regression guard — new rules must fit within this envelope.
//
// The test locates the actual repo root by walking up from the test binary's
// location (which is under atomic/internal/validate/ during `go test`). If no
// .git is found (e.g. running outside the repo), the test is skipped rather
// than failing.
func TestPerfBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping perf test in -short mode")
	}

	// Find the repo root from the test's working directory.
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

// findRepoRootForTest walks up from startDir looking for .git (file or dir).
// Duplicates the internal findRepoRoot to avoid importing from the same package
// under test (it's already in the package under test via white-box access but
// this is an _test package).
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
