package doctor

import (
	"sync/atomic"
	"testing"
)

// TestRunWith_PresetRepoRoot_NoGitCalls proves that when opts.RepoRoot is
// pre-populated, RunWith does NOT invoke gitToplevelFn at all — the cached
// value is used as-is across all 11 checks.
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

	// repoDev=true to include the manifest check; all 11 checks run.
	if _, err := RunWith(opts, true); err != nil {
		t.Fatalf("RunWith: %v", err)
	}

	if n := calls.Load(); n != 0 {
		t.Errorf("gitToplevelFn called %d times during RunWith with pre-set RepoRoot; want 0", n)
	}
}

// TestRunWith_LazyFill_ExactlyOneGitCall proves that when opts.RepoRoot is
// empty, RunWith resolves it exactly once — not once per check. This is the
// key invariant that proves the fan-out was eliminated: before the fix each of
// N checks spawned its own git subprocess; after the fix RunWith's lazy-fill
// path fires exactly once and all checks reuse opts.RepoRoot.
func TestRunWith_LazyFill_ExactlyOneGitCall(t *testing.T) {
	var calls atomic.Int32

	orig := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = orig })

	gitToplevelFn = func(cwd string) string {
		calls.Add(1)
		return orig(cwd)
	}

	// Empty RepoRoot triggers the lazy-fill path inside RunWith.
	opts := Opts{StaleDays: 7}

	// repoDev=true to include the manifest check; all 11 checks run.
	if _, err := RunWith(opts, true); err != nil {
		t.Fatalf("RunWith: %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("gitToplevelFn called %d times during RunWith with empty RepoRoot; want exactly 1", n)
	}
}

// TestRun_GitToplevelCalledExactlyOnce verifies that the public Run entry
// point resolves the git toplevel exactly once. Asserting == 1 (not ≤ 1)
// prevents the test from passing vacuously if a future refactor stops
// resolving the root entirely.
func TestRun_GitToplevelCalledExactlyOnce(t *testing.T) {
	var calls atomic.Int32

	orig := gitToplevelFn
	t.Cleanup(func() { gitToplevelFn = orig })

	gitToplevelFn = func(cwd string) string {
		calls.Add(1)
		return orig(cwd)
	}

	opts := Opts{StaleDays: 7}
	// Do not set RepoRoot so that Run resolves it (exactly once).

	if _, err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("gitToplevelFn called %d times during Run; want exactly 1", n)
	}
}
