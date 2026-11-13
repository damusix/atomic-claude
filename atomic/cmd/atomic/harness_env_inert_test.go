package main

import (
	"os"
	"os/exec"
	"testing"
)

// ATOMIC_HARNESS is retired: whatever it holds, a verb must behave exactly as
// it does with the variable unset, including the exit code.
func TestRootVerbsIgnoreAtomicHarness(t *testing.T) {
	// Restored on cleanup; leaves the variable truly unset for the baseline.
	t.Setenv("ATOMIC_HARNESS", "")
	if err := os.Unsetenv("ATOMIC_HARNESS"); err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	verbs := [][]string{
		{"where", "--json"},
		{"state", "adopt", "--dry-run"},
		{"hooks", "session-start"},
		{"install", "--harness", "claude", "--dry-run"},
	}
	for _, args := range verbs {
		argv := append([]string{"root"}, args...)
		argv = append(argv, "--repo", repo)
		home := t.TempDir()
		wantOut, wantCode := runAtomicCLIEnv(t, home, nil, argv...)
		if wantCode != 0 || wantOut == "" {
			t.Fatalf("%v unset baseline: exit %d, output %q; want exit 0 and output", args, wantCode, wantOut)
		}
		for _, value := range []string{"claude", "omp", "bogus"} {
			out, code := runAtomicCLIEnv(t, home, []string{"ATOMIC_HARNESS=" + value}, argv...)
			if code != wantCode || out != wantOut {
				t.Errorf("%v with ATOMIC_HARNESS=%s: exit %d, output %q; unset: exit %d, output %q",
					args, value, code, out, wantCode, wantOut)
			}
		}
	}
}
