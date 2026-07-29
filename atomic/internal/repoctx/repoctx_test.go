package repoctx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
)

// Happy path: override path — resolves to an absolute path when given a valid dir.
func TestResolve_Override(t *testing.T) {
	dir := t.TempDir()
	got, err := repoctx.Resolve(dir)
	if err != nil {
		t.Fatalf("Resolve(%q) error: %v", dir, err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned non-absolute path: %q", got)
	}
	if got != dir {
		t.Errorf("Resolve(%q) = %q, want same dir", dir, got)
	}
}

// Override with a relative path is resolved to absolute.
// We save/restore cwd so the chdir doesn't bleed into parallel tests.
func TestResolve_OverrideRelative(t *testing.T) {
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got, err := repoctx.Resolve(".")
	if err != nil {
		t.Fatalf("Resolve(\".\") error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned non-absolute path: %q", got)
	}
}

// Override with a non-existent path returns an error.
func TestResolve_OverrideNotExist(t *testing.T) {
	_, err := repoctx.Resolve("/does/not/exist/xyzzy-atomic-test")
	if err == nil {
		t.Fatal("expected error for non-existent override path, got nil")
	}
}

// No override, inside a git repo: should return the repo root.
func TestResolve_GitRepo(t *testing.T) {
	// We are inside the claude-code-setup repo; resolve from cwd.
	got, err := repoctx.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") in git repo error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned non-absolute path: %q", got)
	}
}

// No override, outside a git repo: falls back to the current working directory.
//
// Git is a history substrate, not a precondition for atomic. When `git rev-parse`
// fails (no repo), Resolve returns the cwd so commands operate on the cwd tree
// instead of hard-failing.
//
// Assumption: t.TempDir() returns a path that is not inside any git repository.
// On some CI setups the temp directory may live under /home or /tmp which could
// be inside a git tree; in that case the test is skipped rather than producing a
// false negative.
func TestResolve_NotInGitRepo_FallsBackToCwd(t *testing.T) {
	dir := t.TempDir()

	// Verify the assumption: the temp dir must not be inside a git repo.
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		root := strings.TrimSpace(string(out))
		t.Skipf("temp dir %q is inside a git repo (%s); cannot test no-git case here", dir, root)
	}

	orig, getErr := os.Getwd()
	if getErr != nil {
		t.Fatal(getErr)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// The cwd Resolve should report — both Resolve and this test call os.Getwd()
	// after the Chdir, so they resolve to the same canonical path.
	wantCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	got, err := repoctx.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") outside a git repo error: %v (want cwd fallback)", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned non-absolute path: %q", got)
	}
	if got != wantCwd {
		t.Errorf("Resolve(\"\") = %q, want cwd %q", got, wantCwd)
	}
}

// --- ResolveFrom ---

// TestResolveFrom_Override: an override short-circuits marker/git resolution
// entirely and reports ScopeSourceNone, regardless of dir.
func TestResolveFrom_Override(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()

	got, src, err := repoctx.ResolveFrom(otherDir, dir)
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if got != dir {
		t.Errorf("ResolveFrom(%q, %q) = %q, want %q", otherDir, dir, got, dir)
	}
	if src != config.ScopeSourceNone {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceNone)
	}
}

// TestResolveFrom_OverrideNotExist: an override that does not exist errors,
// same as Resolve.
func TestResolveFrom_OverrideNotExist(t *testing.T) {
	_, _, err := repoctx.ResolveFrom(t.TempDir(), "/does/not/exist/xyzzy-atomic-test")
	if err == nil {
		t.Fatal("expected error for non-existent override path, got nil")
	}
}

// TestResolveFrom_MarkerWinsOverNoGit: a scope="repo" marker at or above dir
// resolves the root, source marker, with no git repo present at all.
func TestResolveFrom_MarkerWinsOverNoGit(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	if _, err := config.EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, src, err := repoctx.ResolveFrom(nested, "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if got != root {
		t.Errorf("ResolveFrom(%q, \"\") = %q, want marker root %q", nested, got, root)
	}
	if src != config.ScopeSourceMarker {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceMarker)
	}
}

// TestResolveFrom_MarkerWinsOverGitToplevel: a scope="repo" marker nested
// inside a git repository outranks the git toplevel — the marker directory
// is reported, not the (higher-up) git root.
func TestResolveFrom_MarkerWinsOverGitToplevel(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	gitRoot := t.TempDir()
	if err := exec.Command("git", "init", gitRoot).Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	markerRoot := filepath.Join(gitRoot, "server")
	if err := os.MkdirAll(markerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := config.EnsureScopeMarker(markerRoot, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	nested := filepath.Join(markerRoot, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, src, err := repoctx.ResolveFrom(nested, "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if got != markerRoot {
		t.Errorf("ResolveFrom(%q, \"\") = %q, want marker root %q (not git root %q)", nested, got, markerRoot, gitRoot)
	}
	if src != config.ScopeSourceMarker {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceMarker)
	}
}

// TestResolveFrom_MarkerWithRelativeDir: dir passed to ResolveFrom as a
// relative path still resolves to an absolute marker root — the documented
// "returns the absolute path" invariant, defended explicitly in the marker
// branch rather than relying solely on FindScopeRoot's own normalization.
func TestResolveFrom_MarkerWithRelativeDir(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	if _, err := config.EnsureScopeMarker(root, "repo"); err != nil {
		t.Fatalf("EnsureScopeMarker: %v", err)
	}
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	// filepath.Abs on a relative path resolves via os.Getwd(), which
	// canonicalizes symlinks (e.g. macOS's /var -> /private/var) — resolve
	// the expected root the same way so the comparison isn't a false
	// negative on such platforms.
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	got, src, err := repoctx.ResolveFrom("src", "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ResolveFrom returned non-absolute path: %q", got)
	}
	if got != wantRoot {
		t.Errorf("ResolveFrom(\"src\", \"\") = %q, want marker root %q", got, wantRoot)
	}
	if src != config.ScopeSourceMarker {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceMarker)
	}
}

// TestResolveFrom_GitFallback_NoMarker: no marker anywhere — falls back to
// git toplevel, source git.
func TestResolveFrom_GitFallback_NoMarker(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	nested := filepath.Join(root, "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, src, err := repoctx.ResolveFrom(nested, "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if src != config.ScopeSourceGit {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceGit)
	}
	wantRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	if gotResolved != wantRoot {
		t.Errorf("ResolveFrom(%q, \"\") = %q, want git root %q", nested, got, root)
	}
}

// TestResolveFrom_CwdFallback_NoMarkerNoGit: neither a marker nor a git repo
// — falls back to dir itself, source cwd.
//
// Assumption: t.TempDir() returns a path that is not inside any git
// repository. Skipped rather than false-negative when that assumption fails.
func TestResolveFrom_CwdFallback_NoMarkerNoGit(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	dir := t.TempDir()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		root := strings.TrimSpace(string(out))
		t.Skipf("temp dir %q is inside a git repo (%s); cannot test no-git case here", dir, root)
	}

	got, src, err := repoctx.ResolveFrom(dir, "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if got != dir {
		t.Errorf("ResolveFrom(%q, \"\") = %q, want %q", dir, got, dir)
	}
	if src != config.ScopeSourceCwd {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceCwd)
	}
}

// TestResolveFrom_GitRunsInGivenDir_NotProcessCwd is the decisive proof that
// ResolveFrom runs "git rev-parse --show-toplevel" in dir (cmd.Dir), not the
// process's own cwd — otherwise the directory parameter would be a lie. Two
// distinct git repos: the process stays chdir'd into repoA the whole time,
// while dir=repoB is passed explicitly. The result must be repoB's toplevel.
func TestResolveFrom_GitRunsInGivenDir_NotProcessCwd(t *testing.T) {
	restore := config.SetHarnessDirForTest(".claude")
	defer restore()

	repoA := t.TempDir()
	if err := exec.Command("git", "init", repoA).Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	repoB := t.TempDir()
	if err := exec.Command("git", "init", repoB).Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repoA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	got, src, err := repoctx.ResolveFrom(repoB, "")
	if err != nil {
		t.Fatalf("ResolveFrom error: %v", err)
	}
	if src != config.ScopeSourceGit {
		t.Errorf("source = %q, want %q", src, config.ScopeSourceGit)
	}

	wantB, err := filepath.EvalSymlinks(repoB)
	if err != nil {
		t.Fatal(err)
	}
	gotResolved, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	if gotResolved != wantB {
		t.Errorf("ResolveFrom ran git in the process cwd instead of dir: got %q, want repoB's toplevel %q", got, repoB)
	}
}
