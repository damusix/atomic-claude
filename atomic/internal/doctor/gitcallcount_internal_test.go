package doctor

import (
	"sync/atomic"
	"testing"
)

// A pre-set RepoRoot must be used as-is: zero git subprocesses.
func TestRunWith_PresetRepoRoot_NoGitCalls(t *testing.T) {
	var calls atomic.Int32

	orig := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = orig })

	gitToplevelFn = func(cwd string) string {
		calls.Add(1)
		return orig(cwd)
	}

	root := t.TempDir()
	opts := Opts{
		StaleDays: 7,
		RepoRoot:  root, // pre-set: RunWith must NOT call gitToplevelFn
	}

	// repoDev=true so the manifest check runs too.
	if _, err := RunWith(opts, true); err != nil {
		t.Fatalf("RunWith: %v", err)
	}

	if n := calls.Load(); n != 0 {
		t.Errorf("gitToplevelFn called %d times during RunWith with pre-set RepoRoot; want 0", n)
	}
}

// The lazy-fill path must resolve once for the whole run, not once per check.
func TestRunWith_LazyFill_ExactlyOneGitCall(t *testing.T) {
	var calls atomic.Int32

	orig := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = orig })

	gitToplevelFn = func(cwd string) string {
		calls.Add(1)
		return orig(cwd)
	}

	// An empty RepoRoot is what triggers the lazy-fill path.
	opts := Opts{StaleDays: 7}

	// repoDev=true so the manifest check runs too.
	if _, err := RunWith(opts, true); err != nil {
		t.Fatalf("RunWith: %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("gitToplevelFn called %d times during RunWith with empty RepoRoot; want exactly 1", n)
	}
}

// Asserting == 1 rather than <= 1 keeps this from passing vacuously if a
// refactor stops resolving the root at all.
func TestRun_GitToplevelCalledExactlyOnce(t *testing.T) {
	var calls atomic.Int32

	orig := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = orig })

	gitToplevelFn = func(cwd string) string {
		calls.Add(1)
		return orig(cwd)
	}

	// RepoRoot is left unset so Run has to resolve it.
	opts := Opts{StaleDays: 7}

	if _, err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("gitToplevelFn called %d times during Run; want exactly 1", n)
	}
}
