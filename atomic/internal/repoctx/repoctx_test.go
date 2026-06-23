package repoctx_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
