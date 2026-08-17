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

func TestResolve_OverrideNotExist(t *testing.T) {
	_, err := repoctx.Resolve("/does/not/exist/xyzzy-atomic-test")
	if err == nil {
		t.Fatal("expected error for non-existent override path, got nil")
	}
}

func TestResolve_GitRepo(t *testing.T) {
	got, err := repoctx.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\") in git repo error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Resolve returned non-absolute path: %q", got)
	}
}

// Git is a history substrate, not a precondition: with no repo, Resolve returns
// the cwd rather than failing. Skips when t.TempDir() happens to sit inside a
// git tree, which some CI layouts produce.
func TestResolve_NotInGitRepo_FallsBackToCwd(t *testing.T) {
	dir := t.TempDir()

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

	// Both sides call os.Getwd() after the Chdir, so they canonicalize alike.
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

func TestResolveFrom_OverrideNotExist(t *testing.T) {
	_, _, err := repoctx.ResolveFrom(t.TempDir(), "/does/not/exist/xyzzy-atomic-test")
	if err == nil {
		t.Fatal("expected error for non-existent override path, got nil")
	}
}

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

	// filepath.Abs goes through os.Getwd(), which canonicalizes symlinks
	// (macOS /var -> /private/var), so the expected root must too.
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

// Skips when t.TempDir() happens to sit inside a git tree.
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

// git must run in dir, not the process cwd, or the dir parameter is a lie: the
// process stays inside repoA throughout while dir=repoB must win.
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
